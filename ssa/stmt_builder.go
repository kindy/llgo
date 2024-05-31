/*
 * Copyright (c) 2024 The GoPlus Authors (goplus.org). All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package ssa

import (
	"bytes"
	"fmt"
	"go/types"
	"log"
	"strings"

	"github.com/goplus/llvm"
)

// -----------------------------------------------------------------------------

type aBasicBlock struct {
	first llvm.BasicBlock // a Go SSA basic block may have multiple LLVM basic blocks
	last  llvm.BasicBlock
	fn    Function
	idx   int
}

// BasicBlock represents a basic block in a function.
type BasicBlock = *aBasicBlock

// Parent returns the function to which the basic block belongs.
func (p BasicBlock) Parent() Function {
	return p.fn
}

// Index returns the index of the basic block in the parent function.
func (p BasicBlock) Index() int {
	return p.idx
}

// -----------------------------------------------------------------------------

type aBuilder struct {
	impl llvm.Builder
	blk  BasicBlock
	Func Function
	Pkg  Package
	Prog Program
}

// Builder represents a builder for creating instructions in a function.
type Builder = *aBuilder

// Dispose disposes of the builder.
func (b Builder) Dispose() {
	b.impl.Dispose()
}

// SetBlock means SetBlockEx(blk, AtEnd, true).
func (b Builder) SetBlock(blk BasicBlock) Builder {
	if debugInstr {
		log.Printf("Block _llgo_%v:\n", blk.idx)
	}
	b.SetBlockEx(blk, AtEnd, true)
	return b
}

type InsertPoint int

const (
	AtEnd InsertPoint = iota
	AtStart
	BeforeLast
	afterInit
)

// SetBlockEx sets blk as current basic block and pos as its insert point.
func (b Builder) SetBlockEx(blk BasicBlock, pos InsertPoint, setBlk bool) Builder {
	if b.Func != blk.fn {
		panic("mismatched function")
	}
	switch pos {
	case AtEnd:
		b.impl.SetInsertPointAtEnd(blk.last)
	case AtStart:
		b.impl.SetInsertPointBefore(blk.first.FirstInstruction())
	case BeforeLast:
		b.impl.SetInsertPointBefore(blk.last.LastInstruction())
	case afterInit:
		b.impl.SetInsertPointBefore(instrAfterInit(blk.first))
	default:
		panic("SetBlockEx: invalid pos")
	}
	if setBlk {
		b.blk = blk
	}
	return b
}

func instrAfterInit(blk llvm.BasicBlock) llvm.Value {
	instr := blk.FirstInstruction()
	for {
		instr = llvm.NextInstruction(instr)
		if notInit(instr) {
			return instr
		}
	}
}

func notInit(instr llvm.Value) bool {
	switch op := instr.InstructionOpcode(); op {
	case llvm.Call:
		if n := instr.OperandsCount(); n == 1 {
			fn := instr.Operand(0)
			return !strings.HasSuffix(fn.Name(), ".init")
		}
	}
	return true
}

// Panic emits a panic instruction.
func (b Builder) Panic(v Expr) {
	if debugInstr {
		log.Printf("Panic %v\n", v.impl)
	}
	b.Call(b.Pkg.rtFunc("TracePanic"), v)
	b.impl.CreateUnreachable()
}

// Unreachable emits an unreachable instruction.
func (b Builder) Unreachable() {
	b.impl.CreateUnreachable()
}

// The Go instruction creates a new goroutine and calls the specified
// function within it.
//
// Example printed form:
//
//	go println(t0, t1)
//	go t3()
//	go invoke t5.Println(...t6)
func (b Builder) Go(fn Expr, args ...Expr) {
	if debugInstr {
		logCall("Go", fn, args)
	}
	b.Call(fn, args...)
}

// Return emits a return instruction.
func (b Builder) Return(results ...Expr) {
	if debugInstr {
		var b bytes.Buffer
		fmt.Fprint(&b, "Return ")
		for i, arg := range results {
			if i > 0 {
				fmt.Fprint(&b, ", ")
			}
			fmt.Fprint(&b, arg.impl)
		}
		log.Println(b.String())
	}
	switch n := len(results); n {
	case 0:
		b.impl.CreateRetVoid()
	case 1:
		raw := b.Func.raw.Type.(*types.Signature).Results().At(0).Type()
		ret := checkExpr(results[0], raw, b)
		b.impl.CreateRet(ret.impl)
	default:
		tret := b.Func.raw.Type.(*types.Signature).Results()
		b.impl.CreateAggregateRet(llvmParams(0, results, tret, b))
	}
}

// The Extract instruction yields component Index of Tuple.
//
// This is used to access the results of instructions with multiple
// return values, such as Call, TypeAssert, Next, UnOp(ARROW) and
// IndexExpr(Map).
//
// Example printed form:
//
//	t1 = extract t0 #1
func (b Builder) Extract(x Expr, i int) (ret Expr) {
	if debugInstr {
		log.Printf("Extract %v, %d\n", x.impl, i)
	}
	return b.getField(x, i)
}

// Jump emits a jump instruction.
func (b Builder) Jump(jmpb BasicBlock) {
	if b.Func != jmpb.fn {
		panic("mismatched function")
	}
	if debugInstr {
		log.Printf("Jump _llgo_%v\n", jmpb.idx)
	}
	b.impl.CreateBr(jmpb.first)
}

// If emits an if instruction.
func (b Builder) If(cond Expr, thenb, elseb BasicBlock) {
	if b.Func != thenb.fn || b.Func != elseb.fn {
		panic("mismatched function")
	}
	if debugInstr {
		log.Printf("If %v, _llgo_%v, _llgo_%v\n", cond.impl, thenb.idx, elseb.idx)
	}
	b.impl.CreateCondBr(cond.impl, thenb.first, elseb.first)
}

// -----------------------------------------------------------------------------

// Phi represents a phi node.
type Phi struct {
	Expr
}

// AddIncoming adds incoming values to a phi node.
func (p Phi) AddIncoming(b Builder, preds []BasicBlock, f func(i int, blk BasicBlock) Expr) {
	bs := llvmPredBlocks(preds)
	vals := make([]llvm.Value, len(preds))
	for iblk, blk := range preds {
		vals[iblk] = f(iblk, blk).impl
	}
	p.impl.AddIncoming(vals, bs)
}

func llvmPredBlocks(preds []BasicBlock) []llvm.BasicBlock {
	ret := make([]llvm.BasicBlock, len(preds))
	for i, v := range preds {
		ret[i] = v.last
	}
	return ret
}

// Phi returns a phi node.
func (b Builder) Phi(t Type) Phi {
	phi := llvm.CreatePHI(b.impl, t.ll)
	return Phi{Expr{phi, t}}
}

// -----------------------------------------------------------------------------

; ModuleID = 'main'
source_filename = "main"

%"github.com/goplus/llgo/internal/runtime.String" = type { ptr, i64 }

@"main.init$guard" = global ptr null

define void @main.init() {
_llgo_0:
  %0 = load i1, ptr @"main.init$guard", align 1
  br i1 %0, label %_llgo_2, label %_llgo_1

_llgo_1:                                          ; preds = %_llgo_0
  store i1 true, ptr @"main.init$guard", align 1
  br label %_llgo_2

_llgo_2:                                          ; preds = %_llgo_1, %_llgo_0
  ret void
}

define void @main() {
_llgo_0:
  call void @main.init()
  %0 = call ptr @"github.com/goplus/llgo/internal/runtime/c.String"([4 x i8] c"Hi\0A\00")
  %1 = alloca i8, i64 4, align 1
  %2 = call ptr @memcpy(ptr %1, ptr %0, i64 4)
  %3 = call ptr @"github.com/goplus/llgo/internal/runtime/c.String"([3 x i8] c"%s\00")
  %4 = call i32 (ptr, ...) @printf(ptr %3, ptr %0)
  ret void
}

declare ptr @"github.com/goplus/llgo/internal/runtime/c.String"(%"github.com/goplus/llgo/internal/runtime.String")

declare ptr @memcpy(ptr, ptr, i64)

declare i32 @printf(ptr, ...)

package util

import (
	"fmt"
	"runtime"
	"strings"
)

// CallerInfo 表示调用者信息
type CallerInfo struct {
	File     string // 文件名
	Line     int    // 行号
	Function string // 函数名
	Package  string // 包名
}

// GetCallerInfo 获取调用栈信息
// skip 表示要跳过的调用栈帧数，0表示当前函数，1表示调用当前函数的函数，以此类推
func GetCallerInfo(skip int) CallerInfo {
	pc, file, line, ok := runtime.Caller(skip + 1)
	if !ok {
		return CallerInfo{
			File:     "unknown",
			Line:     0,
			Function: "unknown",
			Package:  "unknown",
		}
	}

	// 获取函数名
	fn := runtime.FuncForPC(pc)
	funcName := "unknown"
	pkgName := "unknown"
	
	if fn != nil {
		funcName = fn.Name()
		// 分离包名和函数名
		if lastDot := strings.LastIndex(funcName, "."); lastDot >= 0 {
			pkgName = funcName[:lastDot]
			funcName = funcName[lastDot+1:]
		}
	}

	// 获取短文件名
	if lastSlash := strings.LastIndex(file, "/"); lastSlash >= 0 {
		file = file[lastSlash+1:]
	} else if lastSlash = strings.LastIndex(file, "\\"); lastSlash >= 0 {
		file = file[lastSlash+1:]
	}

	return CallerInfo{
		File:     file,
		Line:     line,
		Function: funcName,
		Package:  pkgName,
	}
}

// GetCallerInfoString 获取格式化的调用者信息字符串
func GetCallerInfoString(skip int) string {
	info := GetCallerInfo(skip + 1)
	return fmt.Sprintf("%s:%d[%s]", info.File, info.Line, info.Function)
}
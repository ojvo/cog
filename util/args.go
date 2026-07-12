package util

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// 错误定义
var (
	ArgErrMissingValue = errors.New("missing value")
	ArgErrInvalidValue = errors.New("invalid value")
	ArgErrDuplicate    = errors.New("duplicate argument")
	ArgErrRequired     = errors.New("required argument missing")
	ArgErrUnknown      = errors.New("unknown argument")
	ArgErrInvalidSetup = errors.New("invalid setup")
)

// ArgParseResult 解析结果
type ArgParseResult struct {
	errors   []error
	warnings []string
}

func (r *ArgParseResult) HasErrors() bool    { return len(r.errors) > 0 }
func (r *ArgParseResult) HasWarnings() bool  { return len(r.warnings) > 0 }
func (r *ArgParseResult) Errors() []error    { return append([]error(nil), r.errors...) }
func (r *ArgParseResult) Warnings() []string { return append([]string(nil), r.warnings...) }

func (r *ArgParseResult) Error() string {
	if len(r.errors) == 0 {
		return ""
	}
	var msgs []string
	for _, err := range r.errors {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

func (r *ArgParseResult) addError(err error) {
	if err != nil {
		r.errors = append(r.errors, err)
	}
}

func (r *ArgParseResult) addWarning(msg string) {
	if msg != "" {
		r.warnings = append(r.warnings, msg)
	}
}

// ArgHandler 参数处理函数类型
type ArgHandler func(args []string, index int) (consumed int, err error)

// ArgDefinition 参数定义
type ArgDefinition struct {
	names    []string
	handler  ArgHandler
	required bool
	used     bool
	help     string
	argType  string
}

// ArgParser 函数式解析器（保留这一个就够了）
type ArgParser struct {
	args         []*ArgDefinition
	argMap       map[string]*ArgDefinition
	trailingVar  *[]string
	trailingHelp string
	result       *ArgParseResult
	programName  string
	description  string
	strictMode   bool
}

// NewArgParser 创建新解析器
func NewArgParser() *ArgParser {
	return &ArgParser{
		argMap: make(map[string]*ArgDefinition),
		result: &ArgParseResult{},
	}
}

// Program 设置程序信息
func (p *ArgParser) Program(name, description string) *ArgParser {
	p.programName = strings.TrimSpace(name)
	p.description = strings.TrimSpace(description)
	return p
}

// Strict 设置严格模式
func (p *ArgParser) Strict() *ArgParser {
	p.strictMode = true
	return p
}

// Bool 添加布尔选项
func (p *ArgParser) Bool(target *bool, names string) *ArgParser {
	return p.BoolHelp(target, names, "")
}

func (p *ArgParser) BoolHelp(target *bool, names, help string) *ArgParser {
	if target == nil {
		p.result.addError(fmt.Errorf("bool target cannot be nil"))
		return p
	}
	
	nameList := p.parseNames(names)
	if len(nameList) == 0 {
		p.result.addError(fmt.Errorf("bool argument needs at least one name"))
		return p
	}

	handler := func(args []string, index int) (int, error) {
		argName := args[index]
		*target = !strings.Contains(argName, "--no-")
		return 0, nil
	}

	p.registerArgument(&ArgDefinition{
		names:   p.normalizeNames(nameList),
		handler: handler,
		help:    help,
		argType: "bool",
	})
	return p
}

// String 添加字符串参数
func (p *ArgParser) String(target *string, names string) *ArgParser {
	return p.StringHelp(target, names, "")
}

func (p *ArgParser) StringRequired(target *string, names string) *ArgParser {
	return p.StringRequiredHelp(target, names, "")
}

func (p *ArgParser) StringHelp(target *string, names, help string) *ArgParser {
	return p.stringArg(target, false, names, help)
}

func (p *ArgParser) StringRequiredHelp(target *string, names, help string) *ArgParser {
	return p.stringArg(target, true, names, help)
}

func (p *ArgParser) stringArg(target *string, required bool, names, help string) *ArgParser {
	if target == nil {
		p.result.addError(fmt.Errorf("string target cannot be nil"))
		return p
	}
	
	nameList := p.parseNames(names)
	if len(nameList) == 0 {
		p.result.addError(fmt.Errorf("string argument needs at least one name"))
		return p
	}

	handler := func(args []string, index int) (int, error) {
		if index+1 >= len(args) {
			return 0, fmt.Errorf("%w for %s", ArgErrMissingValue, args[index])
		}
		nextArg := args[index+1]
		if strings.HasPrefix(nextArg, "-") && nextArg != "-" && nextArg != "--" {
			return 0, fmt.Errorf("%w for %s", ArgErrMissingValue, args[index])
		}
		*target = nextArg
		return 1, nil
	}

	p.registerArgument(&ArgDefinition{
		names:    p.normalizeNames(nameList),
		handler:  handler,
		required: required,
		help:     help,
		argType:  "<string>",
	})
	return p
}

// Int 添加整数参数
func (p *ArgParser) Int(target *int, names string) *ArgParser {
	return p.IntHelp(target, names, "")
}

func (p *ArgParser) IntRequired(target *int, names string) *ArgParser {
	return p.IntRequiredHelp(target, names, "")
}

func (p *ArgParser) IntHelp(target *int, names, help string) *ArgParser {
	return p.intArg(target, false, names, help)
}

func (p *ArgParser) IntRequiredHelp(target *int, names, help string) *ArgParser {
	return p.intArg(target, true, names, help)
}

func (p *ArgParser) intArg(target *int, required bool, names, help string) *ArgParser {
	if target == nil {
		p.result.addError(fmt.Errorf("int target cannot be nil"))
		return p
	}
	
	nameList := p.parseNames(names)
	if len(nameList) == 0 {
		p.result.addError(fmt.Errorf("int argument needs at least one name"))
		return p
	}

	handler := func(args []string, index int) (int, error) {
		if index+1 >= len(args) {
			return 0, fmt.Errorf("%w for %s", ArgErrMissingValue, args[index])
		}
		nextArg := args[index+1]
		if strings.HasPrefix(nextArg, "-") && nextArg != "-" && nextArg != "--" {
			return 0, fmt.Errorf("%w for %s", ArgErrMissingValue, args[index])
		}
		
		val, err := strconv.Atoi(nextArg)
		if err != nil {
			return 0, fmt.Errorf("%w: '%s' is not a valid integer", ArgErrInvalidValue, nextArg)
		}
		*target = val
		return 1, nil
	}

	p.registerArgument(&ArgDefinition{
		names:    p.normalizeNames(nameList),
		handler:  handler,
		required: required,
		help:     help,
		argType:  "<int>",
	})
	return p
}

// Int64 添加int64参数
func (p *ArgParser) Int64(target *int64, names string) *ArgParser {
	return p.Int64Help(target, names, "")
}

func (p *ArgParser) Int64Required(target *int64, names string) *ArgParser {
	return p.Int64RequiredHelp(target, names, "")
}

func (p *ArgParser) Int64Help(target *int64, names, help string) *ArgParser {
	return p.int64Arg(target, false, names, help)
}

func (p *ArgParser) Int64RequiredHelp(target *int64, names, help string) *ArgParser {
	return p.int64Arg(target, true, names, help)
}

func (p *ArgParser) int64Arg(target *int64, required bool, names, help string) *ArgParser {
	if target == nil {
		p.result.addError(fmt.Errorf("int64 target cannot be nil"))
		return p
	}
	
	nameList := p.parseNames(names)
	if len(nameList) == 0 {
		p.result.addError(fmt.Errorf("int64 argument needs at least one name"))
		return p
	}

	handler := func(args []string, index int) (int, error) {
		if index+1 >= len(args) {
			return 0, fmt.Errorf("%w for %s", ArgErrMissingValue, args[index])
		}
		nextArg := args[index+1]
		if strings.HasPrefix(nextArg, "-") && nextArg != "-" && nextArg != "--" {
			return 0, fmt.Errorf("%w for %s", ArgErrMissingValue, args[index])
		}
		
		val, err := strconv.ParseInt(nextArg, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%w: '%s' is not a valid int64", ArgErrInvalidValue, nextArg)
		}
		*target = val
		return 1, nil
	}

	p.registerArgument(&ArgDefinition{
		names:    p.normalizeNames(nameList),
		handler:  handler,
		required: required,
		help:     help,
		argType:  "<int64>",
	})
	return p
}

// Uint 添加无符号整数参数
func (p *ArgParser) Uint(target *uint, names string) *ArgParser {
	return p.UintHelp(target, names, "")
}

func (p *ArgParser) UintRequired(target *uint, names string) *ArgParser {
	return p.UintRequiredHelp(target, names, "")
}

func (p *ArgParser) UintHelp(target *uint, names, help string) *ArgParser {
	return p.uintArg(target, false, names, help)
}

func (p *ArgParser) UintRequiredHelp(target *uint, names, help string) *ArgParser {
	return p.uintArg(target, true, names, help)
}

func (p *ArgParser) uintArg(target *uint, required bool, names, help string) *ArgParser {
	if target == nil {
		p.result.addError(fmt.Errorf("uint target cannot be nil"))
		return p
	}
	
	nameList := p.parseNames(names)
	if len(nameList) == 0 {
		p.result.addError(fmt.Errorf("uint argument needs at least one name"))
		return p
	}

	handler := func(args []string, index int) (int, error) {
		if index+1 >= len(args) {
			return 0, fmt.Errorf("%w for %s", ArgErrMissingValue, args[index])
		}
		nextArg := args[index+1]
		if strings.HasPrefix(nextArg, "-") && nextArg != "-" && nextArg != "--" {
			return 0, fmt.Errorf("%w for %s", ArgErrMissingValue, args[index])
		}
		
		val, err := strconv.ParseUint(nextArg, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%w: '%s' is not a valid uint", ArgErrInvalidValue, nextArg)
		}
		*target = uint(val)
		return 1, nil
	}

	p.registerArgument(&ArgDefinition{
		names:    p.normalizeNames(nameList),
		handler:  handler,
		required: required,
		help:     help,
		argType:  "<uint>",
	})
	return p
}

// Float 添加浮点数参数
func (p *ArgParser) Float(target *float64, names string) *ArgParser {
	return p.FloatHelp(target, names, "")
}

func (p *ArgParser) FloatRequired(target *float64, names string) *ArgParser {
	return p.FloatRequiredHelp(target, names, "")
}

func (p *ArgParser) FloatHelp(target *float64, names, help string) *ArgParser {
	return p.floatArg(target, false, names, help)
}

func (p *ArgParser) FloatRequiredHelp(target *float64, names, help string) *ArgParser {
	return p.floatArg(target, true, names, help)
}

func (p *ArgParser) floatArg(target *float64, required bool, names, help string) *ArgParser {
	if target == nil {
		p.result.addError(fmt.Errorf("float64 target cannot be nil"))
		return p
	}
	
	nameList := p.parseNames(names)
	if len(nameList) == 0 {
		p.result.addError(fmt.Errorf("float argument needs at least one name"))
		return p
	}

	handler := func(args []string, index int) (int, error) {
		if index+1 >= len(args) {
			return 0, fmt.Errorf("%w for %s", ArgErrMissingValue, args[index])
		}
		nextArg := args[index+1]
		if strings.HasPrefix(nextArg, "-") && nextArg != "-" && nextArg != "--" {
			return 0, fmt.Errorf("%w for %s", ArgErrMissingValue, args[index])
		}
		
		val, err := strconv.ParseFloat(nextArg, 64)
		if err != nil {
			return 0, fmt.Errorf("%w: '%s' is not a valid float", ArgErrInvalidValue, nextArg)
		}
		*target = val
		return 1, nil
	}

	p.registerArgument(&ArgDefinition{
		names:    p.normalizeNames(nameList),
		handler:  handler,
		required: required,
		help:     help,
		argType:  "<float>",
	})
	return p
}

// Strings 添加字符串切片参数
func (p *ArgParser) Strings(target *[]string, names string) *ArgParser {
	return p.StringsHelp(target, names, "")
}

func (p *ArgParser) StringsRequired(target *[]string, names string) *ArgParser {
	return p.StringsRequiredHelp(target, names, "")
}

func (p *ArgParser) StringsHelp(target *[]string, names, help string) *ArgParser {
	return p.stringsArg(target, false, names, help)
}

func (p *ArgParser) StringsRequiredHelp(target *[]string, names, help string) *ArgParser {
	return p.stringsArg(target, true, names, help)
}

func (p *ArgParser) stringsArg(target *[]string, required bool, names, help string) *ArgParser {
	if target == nil {
		p.result.addError(fmt.Errorf("[]string target cannot be nil"))
		return p
	}
	
	nameList := p.parseNames(names)
	if len(nameList) == 0 {
		p.result.addError(fmt.Errorf("strings argument needs at least one name"))
		return p
	}

	handler := func(args []string, index int) (int, error) {
		consumed := 0
		values := []string{}
		
		for i := index + 1; i < len(args); i++ {
			arg := args[i]
			if strings.HasPrefix(arg, "-") && arg != "-" {
				break
			}
			values = append(values, arg)
			consumed++
		}
		
		if len(values) == 0 {
			return 0, fmt.Errorf("%w for %s", ArgErrMissingValue, args[index])
		}
		
		*target = values
		return consumed, nil
	}

	p.registerArgument(&ArgDefinition{
		names:    p.normalizeNames(nameList),
		handler:  handler,
		required: required,
		help:     help,
		argType:  "<strings>",
	})
	return p
}

// Ints 添加整数切片参数
func (p *ArgParser) Ints(target *[]int, names string) *ArgParser {
	return p.IntsHelp(target, names, "")
}

func (p *ArgParser) IntsRequired(target *[]int, names string) *ArgParser {
	return p.IntsRequiredHelp(target, names, "")
}

func (p *ArgParser) IntsHelp(target *[]int, names, help string) *ArgParser {
	return p.intsArg(target, false, names, help)
}

func (p *ArgParser) IntsRequiredHelp(target *[]int, names, help string) *ArgParser {
	return p.intsArg(target, true, names, help)
}

func (p *ArgParser) intsArg(target *[]int, required bool, names, help string) *ArgParser {
	if target == nil {
		p.result.addError(fmt.Errorf("[]int target cannot be nil"))
		return p
	}
	
	nameList := p.parseNames(names)
	if len(nameList) == 0 {
		p.result.addError(fmt.Errorf("ints argument needs at least one name"))
		return p
	}

	handler := func(args []string, index int) (int, error) {
		consumed := 0
		values := []int{}
		
		for i := index + 1; i < len(args); i++ {
			arg := args[i]
			if strings.HasPrefix(arg, "-") && arg != "-" {
				break
			}
			
			val, err := strconv.Atoi(arg)
			if err != nil {
				return 0, fmt.Errorf("%w: '%s' is not a valid integer", ArgErrInvalidValue, arg)
			}
			
			values = append(values, val)
			consumed++
		}
		
		if len(values) == 0 {
			return 0, fmt.Errorf("%w for %s", ArgErrMissingValue, args[index])
		}
		
		*target = values
		return consumed, nil
	}

	p.registerArgument(&ArgDefinition{
		names:    p.normalizeNames(nameList),
		handler:  handler,
		required: required,
		help:     help,
		argType:  "<ints>",
	})
	return p
}

// Trailing 设置非选项参数处理器
func (p *ArgParser) Trailing(target *[]string, help string) *ArgParser {
	if target == nil {
		p.result.addError(fmt.Errorf("trailing target cannot be nil"))
		return p
	}
	
	if p.trailingVar != nil {
		p.result.addError(fmt.Errorf("trailing arguments already defined"))
		return p
	}
	
	p.trailingVar = target
	p.trailingHelp = help
	return p
}

// Custom 添加自定义处理器
func (p *ArgParser) Custom(handler ArgHandler, names, help string) *ArgParser {
	if handler == nil {
		p.result.addError(fmt.Errorf("custom handler cannot be nil"))
		return p
	}
	
	nameList := p.parseNames(names)
	if len(nameList) == 0 {
		p.result.addError(fmt.Errorf("custom argument needs at least one name"))
		return p
	}

	p.registerArgument(&ArgDefinition{
		names:   p.normalizeNames(nameList),
		handler: handler,
		help:    help,
		argType: "<custom>",
	})
	return p
}

// parseNames 解析名字字符串
func (p *ArgParser) parseNames(names string) []string {
	names = strings.TrimSpace(names)
	if names == "" {
		return nil
	}
	
	parts := strings.FieldsFunc(names, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	
	var result []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	
	return result
}

// normalizeNames 标准化参数名
func (p *ArgParser) normalizeNames(names []string) []string {
	var normalized []string
	seenNames := make(map[string]bool)
	
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		
		if strings.HasPrefix(name, "---") {
			p.result.addError(fmt.Errorf("invalid argument name: %s (too many dashes)", name))
			continue
		}
		
		if !strings.HasPrefix(name, "-") {
			if len(name) == 1 {
				name = "-" + name
			} else {
				name = "--" + name
			}
		}
		
		if !p.isValidArgName(name) {
			p.result.addError(fmt.Errorf("invalid argument name: %s", name))
			continue
		}
		
		if !seenNames[name] {
			normalized = append(normalized, name)
			seenNames[name] = true
			
			if strings.HasPrefix(name, "--") && !strings.HasPrefix(name, "--no-") {
				noName := "--no-" + name[2:]
				if !seenNames[noName] {
					normalized = append(normalized, noName)
					seenNames[noName] = true
				}
			}
		}
	}
	return normalized
}

// isValidArgName 验证参数名是否有效
func (p *ArgParser) isValidArgName(name string) bool {
	if name == "-" || name == "--" {
		return false
	}
	
	if strings.HasPrefix(name, "-") && len(name) == 2 {
		char := name[1]
		return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
	}
	
	if strings.HasPrefix(name, "--") && len(name) > 2 {
		rest := name[2:]
		for _, char := range rest {
			if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || 
				 (char >= '0' && char <= '9') || char == '-' || char == '_') {
				return false
			}
		}
		return true
	}
	
	return false
}

// registerArgument 注册参数
func (p *ArgParser) registerArgument(arg *ArgDefinition) {
	p.args = append(p.args, arg)
	
	for _, name := range arg.names {
		if _, exists := p.argMap[name]; exists {
			p.result.addError(fmt.Errorf("duplicate argument name: %s", name))
			continue
		}
		p.argMap[name] = arg
	}
}

// Parse 执行解析
func (p *ArgParser) Parse(args []string) *ArgParseResult {
	result := &ArgParseResult{}
	
	if p.result.HasErrors() {
		result.errors = append(result.errors, p.result.errors...)
		return result
	}

	for _, arg := range p.args {
		arg.used = false
	}

	var trailingArgs []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		
		if arg == "--" {
			trailingArgs = append(trailingArgs, args[i+1:]...)
			break
		}
		
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			trailingArgs = append(trailingArgs, arg)
			continue
		}
		
		argument, exists := p.argMap[arg]
		if !exists {
			if p.strictMode {
				result.addError(fmt.Errorf("%w: %s", ArgErrUnknown, arg))
			} else {
				result.addWarning(fmt.Sprintf("unknown argument: %s", arg))
			}
			continue
		}
		
		if argument.used {
			result.addError(fmt.Errorf("%w: %s", ArgErrDuplicate, arg))
			continue
		}
		
		consumed, err := argument.handler(args, i)
		if err != nil {
			result.addError(fmt.Errorf("%s: %w", arg, err))
			continue
		}
		
		argument.used = true
		i += consumed
	}
	
	if len(trailingArgs) > 0 && p.trailingVar != nil {
		*p.trailingVar = trailingArgs
	} else if len(trailingArgs) > 0 {
		for _, arg := range trailingArgs {
			result.addWarning(fmt.Sprintf("ignored trailing argument: %s", arg))
		}
	}
	
	for _, arg := range p.args {
		if arg.required && !arg.used {
			name := arg.names[0]
			for _, n := range arg.names {
				if !strings.HasPrefix(n, "--no-") {
					name = n
					break
				}
			}
			result.addError(fmt.Errorf("%w: %s", ArgErrRequired, name))
		}
	}
	
	return result
}

// Usage 生成使用说明
func (p *ArgParser) Usage() string {
	var builder strings.Builder
	
	if p.programName != "" {
		builder.WriteString("Usage: " + p.programName + " [OPTIONS]")
		if p.trailingVar != nil {
			builder.WriteString(" [ARGS...]")
		}
		builder.WriteString("\n\n")
	}
	
	if p.description != "" {
		builder.WriteString(p.description + "\n\n")
	}
	
	if len(p.args) > 0 {
		builder.WriteString("Options:\n")
		for _, arg := range p.args {
			var nameStr []string
			for _, name := range arg.names {
				if !strings.HasPrefix(name, "--no-") {
					nameStr = append(nameStr, name)
				}
			}
			
			names := strings.Join(nameStr, ", ")
			
			if arg.argType != "" && arg.argType != "bool" {
				names += " " + arg.argType
			}
			
			if arg.required {
				names += " (required)"
			}
			
			builder.WriteString(fmt.Sprintf("  %-28s %s\n", names, arg.help))
		}
	}
	
	if p.trailingVar != nil {
		builder.WriteString(fmt.Sprintf("\nArguments:\n  %-28s %s\n", 
			"[ARGS...]", p.trailingHelp))
	}
	
	return builder.String()
}

// Reset 重置解析器状态（用于复用）
func (p *ArgParser) Reset() *ArgParser {
	for _, arg := range p.args {
		arg.used = false
	}
	return p
}

// MustParse 解析失败时panic（用于测试）
func (p *ArgParser) MustParse(args []string) {
	result := p.Parse(args)
	if result.HasErrors() {
		panic(fmt.Sprintf("argument parsing failed: %s", result.Error()))
	}
}

// 便利函数（保留这一个就够了）
func ArgParse(args []string, setup func(*ArgParser)) (*ArgParseResult, error) {
	if setup == nil {
		return nil, fmt.Errorf("%w: setup function cannot be nil", ArgErrInvalidSetup)
	}
	
	parser := NewArgParser()
	setup(parser)
	result := parser.Parse(args)
	
	if result.HasErrors() {
		return result, fmt.Errorf(result.Error())
	}
	
	return result, nil
}

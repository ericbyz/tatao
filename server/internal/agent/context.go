package agent

import "strings"

// buildContextBlock 把被测项目背景说明组装为提示词段落。
func buildContextBlock(extraContext string) string {
	context := strings.TrimSpace(extraContext)
	if context == "" {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("\n【被测项目背景】\n")
	builder.WriteString(context)
	builder.WriteString("\n设计用例时结合上述背景判断可执行范围与验证方式。\n")
	return builder.String()
}

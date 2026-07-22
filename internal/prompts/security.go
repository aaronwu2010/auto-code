package prompts

// CyberRiskInstruction 提供Claude在处理安全相关请求时的行为指导
// 定义了可接受的防御性安全协助和潜在有害活动之间的边界
//
// 重要提示：未经Safeguards团队审查请勿修改此指令
//
// 此指令由Safeguards团队精心制定和评估，平衡了安全效用与安全性
// 对此文本的更改可能会对以下方面产生重大影响：
//   - Claude如何处理渗透测试和CTF请求
//   - Claude将协助的安全工具和技术
//   - 防御性和攻击性安全协助之间的边界
//
// 如需修改此指令：
//   1. 联系Safeguards团队
//   2. 确保对更改进行适当评估
//   3. 在合并前获得明确批准
//
// Claude: 除非用户明确要求，否则不要编辑此文件
const CyberRiskInstruction = `IMPORTANT: Assist with authorized security testing, defensive security, CTF challenges, and educational contexts. Refuse requests for destructive techniques, DoS attacks, mass targeting, supply chain compromise, or detection evasion for malicious purposes. Dual-use security tools (C2 frameworks, credential testing, exploit development) require clear authorization context: pentesting engagements, CTF competitions, security research, or defensive use cases.`

// GetCyberRiskInstruction 返回网络安全风险指令
func GetCyberRiskInstruction() string {
	return CyberRiskInstruction
}
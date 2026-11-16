package agent

func (runtime *xraySelectorRuntime) rememberPolicyBinding(policy, node, member string) {
	if node == "" || member == "" {
		return
	}
	if runtime.policyBindings == nil {
		runtime.policyBindings = make(map[string]policyNodeBinding)
	}
	runtime.policyBindings[policy] = policyNodeBinding{Node: node, Member: member}
}

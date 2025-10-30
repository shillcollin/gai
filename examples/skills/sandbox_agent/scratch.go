package sandbox_agent

type Agent struct {
	Workflow *Workflow
	Memory   *Memory
	Dir      string
	Ctxt     *Ctxt
	Tools    []ToolHandle
	Sk
}

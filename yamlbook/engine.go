package yamlbook

type Engine struct {
	ID     string          `yaml:"id,omitempty"`
	Output []*EngineOutput `yaml:"output"`
}

func (e *Engine) Log(logLine LogLine) {
	e.Output = append(e.Output, &EngineOutput{Line: logLine})
}

type EngineOutput struct {
	Line LogLine `yaml:"log,flow"`
}

type LogLine struct {
	Depth    int    `yaml:"depth"`
	SelDepth int    `yaml:"seldepth,omitempty"`
	MultiPV  int    `yaml:"multipv,omitempty"`
	CP       int    `yaml:"cp"`
	Mate     int    `yaml:"mate,omitempty"`
	Nodes    int    `yaml:"nodes,omitempty"`
	TBHits   int    `yaml:"tbhits,omitempty"`
	Time     int    `yaml:"time,omitempty"`
	PV       string `yaml:"pv"`
}

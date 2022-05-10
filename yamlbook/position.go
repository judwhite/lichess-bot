package yamlbook

type Position struct {
	FEN   string `yaml:"fen"`
	Moves Moves  `yaml:"moves,omitempty"`
}

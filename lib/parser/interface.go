package parser

type Parser interface {
	Parse(rawMsg string, result any) error
}

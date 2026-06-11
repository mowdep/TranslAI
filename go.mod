module github.com/gabrielfareau/translai

go 1.23

// Dépendances ajoutées au fil des phases via `go get` :
//   github.com/spf13/cobra            (CLI)
//   github.com/asticode/go-astisub    (parse/save SRT)
//   github.com/pemistahl/lingua-go    (détection langue)
//   gopkg.in/yaml.v3                  (config)

require github.com/spf13/cobra v1.10.2

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
)

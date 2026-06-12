module github.com/gabrielfareau/translai

go 1.25.0

// Dépendances ajoutées au fil des phases via `go get` :
//   github.com/spf13/cobra            (CLI)
//   github.com/asticode/go-astisub    (parse/save SRT)
//   github.com/pemistahl/lingua-go    (détection langue)
//   gopkg.in/yaml.v3                  (config)

require (
	github.com/PuerkitoBio/goquery v1.12.0
	github.com/asticode/go-astisub v0.40.0
	github.com/go-chi/chi/v5 v5.3.0
	github.com/pemistahl/lingua-go v1.4.0
	github.com/spf13/cobra v1.10.2
	golang.org/x/text v0.35.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/andybalholm/cascadia v1.3.3 // indirect
	github.com/asticode/go-astikit v0.20.0 // indirect
	github.com/asticode/go-astits v1.8.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/shopspring/decimal v1.3.1 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/exp v0.0.0-20221106115401-f9659909a136 // indirect
	golang.org/x/net v0.52.0 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

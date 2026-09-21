// Command fixturegen regenerates the deterministic fixture file.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"

	"ttsworkbench/internal/rheo"
)

func main() {
	out := flag.String("out", "fixtures/sweeps.json", "output path")
	seed := flag.Int64("seed", 42, "deterministic seed")
	flag.Parse()
	fx := rheo.GenerateFixture(*seed)
	buf, err := json.MarshalIndent(fx, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	buf = append(buf, '\n')
	if err := os.WriteFile(*out, buf, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s (seed %d, %d curves)", *out, *seed, len(fx.Curves))
}

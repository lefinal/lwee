package main

import (
	"fmt"
	"github.com/goombaio/namegenerator"
	"log"
	"os"
	"strconv"
	"time"
)

func main() {
	lines := 1
	if linesStr := os.Getenv("LINES"); linesStr != "" {
		var err error
		lines, err = strconv.Atoi(linesStr)
		if err != nil {
			log.Fatal("cannot parse lines as number")
		}
	}

	seed := time.Now().UTC().UnixNano()
	nameGenerator := namegenerator.NewNameGenerator(seed)

	for i := 0; i < lines; i++ {
		name := nameGenerator.Generate()
		fmt.Println(name)
	}
}

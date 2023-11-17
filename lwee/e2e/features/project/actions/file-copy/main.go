package main

import (
	"flag"
	"io"
	"log"
	"os"
)

func main() {
	log.Println("Hello!")
	srcFilename := flag.String("src", "", "The source filename.")
	dstFilename := flag.String("dst", "", "The destination filename.")
	flag.Parse()

	if *srcFilename == "" {
		log.Fatal("missing source filename")
	}
	if *dstFilename == "" {
		log.Fatal("missing destination filename")
	}

	log.Println("filenames ok")

	srcFile, err := os.Open(*srcFilename)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = srcFile.Close() }()
	dstFile, err := os.OpenFile(*dstFilename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 766)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = dstFile.Close() }()

	log.Println("files open")
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		log.Fatal(err)
	}
}

package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode"

	"github.com/moonshine-ai/moonshine/language-bindings/go/internal/tokenize"
)

func main() {
	header := flag.String("header", "", "path to the Moonshine C API header")
	output := flag.String("output", "", "generated Go output path")
	flag.Parse()

	if err := run(*header, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(header, output string) error {
	if header == "" {
		return fmt.Errorf("header path is required")
	}

	_ = output

	// preprocess the eader and get the macro table
	preprocess := exec.Command("clang", "-dM", "-E", "-x", "c", header)
	macroTable, err := preprocess.Output()
	if err != nil {
		return fmt.Errorf("preprocessing %s failed: %w", header, err)
	}

	_, err = scanMacroTable(macroTable)
	return err
}

type constant string

func (c constant) asCShimConst() string {
	return "C." + string(c)
}

func (c constant) goConstName() (string, error) {
	tokens, err := tokenize.Tokenize(string(c))
	if err != nil {
		return "", fmt.Errorf("failed to tokenize %s: %w", c, err)
	}
	if len(tokens) == 0 {
		return "", fmt.Errorf("no tokens")
	}

	return camelCase(tokens), nil
}

func camelCase(tokens []string) string {
	newTokens := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		tok := []rune(tok)
		tok[0] = unicode.ToUpper(tok[0])
		newTokens = append(newTokens, string(tok))
	}

	return strings.Join(newTokens, "")
}

func scanMacroTable(macroTable []byte) ([]constant, error) {
	constants := make([]constant, 0, 64)
	scanner := bufio.NewScanner(strings.NewReader(string(macroTable)))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "#define MOONSHINE_") {
			continue
		}

		macroDef := strings.TrimPrefix(line, "#define ")
		macroName, replacement, found := strings.Cut(macroDef, " ")
		if !found {
			continue
		}
		replacement = strings.TrimSpace(replacement)
		if replacement == "" {
			continue
		}

		c := constant(macroName)
		shim := c.asCShimConst()
		goName, err := c.goConstName()
		if err != nil {
			fmt.Println(err)
			continue
		}
		constants = append(constants, c)
		fmt.Printf("name=%q replacement=%q output=\" const %s = %s\"\n", macroName, replacement, goName, shim)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading preprocessor output failed: %w", err)
	}

	return constants, nil
}

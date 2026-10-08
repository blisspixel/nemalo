// Package verify enforces the same Go verification gates locally and in native CI.
package verify

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const MinimumCoverage = 80.0

// Coverage computes statement coverage from all records, not rounded display output.
func Coverage(r io.Reader) (float64, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() || scanner.Text() != "mode: atomic" {
		return 0, errors.New("expected an atomic Go coverage profile")
	}
	type block struct {
		statements uint64
		covered    bool
	}
	blocks := make(map[string]block)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 {
			return 0, errors.New("malformed coverage record")
		}
		statements, e1 := strconv.ParseUint(fields[1], 10, 32)
		count, e2 := strconv.ParseUint(fields[2], 10, 64)
		if e1 != nil || e2 != nil {
			return 0, errors.New("invalid coverage counts")
		}
		prior, exists := blocks[fields[0]]
		if exists && prior.statements != statements {
			return 0, errors.New("inconsistent coverage block")
		}
		blocks[fields[0]] = block{statements: statements, covered: prior.covered || count > 0}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	var total, covered uint64
	for _, b := range blocks {
		total += b.statements
		if b.covered {
			covered += b.statements
		}
	}
	if total == 0 {
		return 0, errors.New("coverage profile contains no statements")
	}
	return float64(covered) * 100 / float64(total), nil
}

func CheckCoverage(r io.Reader) error {
	coverage, err := Coverage(r)
	if err != nil {
		return err
	}
	if coverage < MinimumCoverage {
		return fmt.Errorf("coverage %.2f%% is below %.0f%%", coverage, MinimumCoverage)
	}
	return nil
}

type Runner func(context.Context, string, ...string) ([]byte, error)

func Run(ctx context.Context, out io.Writer) error {
	return Steps(ctx, out, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		command := exec.CommandContext(ctx, name, args...)
		return command.CombinedOutput()
	}, os.Open)
}

// Steps is injectable to test gate ordering, failure propagation, and coverage enforcement.
func Steps(ctx context.Context, out io.Writer, run Runner, open func(string) (*os.File, error)) error {
	commands := [][]string{{"gofmt", "-l", "."}, {"go", "build", "./..."}, {"go", "vet", "./..."}, {"go", "tool", "staticcheck", "./..."}, {"go", "test", "./...", "-coverpkg=./...", "-covermode=atomic", "-coverprofile=coverage.out"}, {"go", "tool", "govulncheck", "./..."}}
	for i, command := range commands {
		if _, err := fmt.Fprintln(out, strings.Join(command, " ")); err != nil {
			return err
		}
		output, err := run(ctx, command[0], command[1:]...)
		if len(output) > 0 {
			if _, writeErr := out.Write(output); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			return fmt.Errorf("%s failed: %w", command[0], err)
		}
		if i == 0 && len(bytes.TrimSpace(output)) > 0 {
			return errors.New("gofmt found unformatted files")
		}
		if i == 4 {
			f, err := open("coverage.out")
			if err != nil {
				return err
			}
			coverageErr := CheckCoverage(f)
			closeErr := f.Close()
			if coverageErr != nil {
				return coverageErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
	return nil
}

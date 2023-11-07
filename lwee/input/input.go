package input

import (
	"bufio"
	"context"
	"fmt"
	"github.com/lefinal/meh"
	"os"
	"strings"
)

var readLine = make(chan string, 16)

type Input interface {
	// RequestConfirm prompts the user with the given one for confirmation. If no
	// input was provided, the given default value will be returned.
	//
	// The provided prompt should be in the format of a question, e.g., "Are you sure
	// you want to do this?" or similar. RequestConfirm will append a space and the
	// confirmation options.
	RequestConfirm(ctx context.Context, prompt string, defaultValue bool) (bool, error)

	// Request prompts the user with the given one for an input.
	//
	// The provided prompt should be in the format of "Enter xyz". RequestInput will
	// append colons.
	Request(ctx context.Context, prompt string, allowEmpty bool) (string, error)
}

type Stdin struct {
}

// Consume starts reading input from stdin. It should be only called once.
// Consume blocks until the given context is done or EOF is reached.
func (input *Stdin) Consume(ctx context.Context) {
	var stdinScanner = bufio.NewScanner(os.Stdin)
	for stdinScanner.Scan() {
		select {
		case <-ctx.Done():
			return
		case readLine <- stdinScanner.Text():
		}
	}
}

func (input *Stdin) RequestConfirm(ctx context.Context, prompt string, defaultValue bool) (bool, error) {
	yes := "y"
	no := "n"
	if defaultValue {
		yes = strings.ToUpper(yes)
	} else {
		no = strings.ToUpper(no)
	}
	prompt = fmt.Sprintf("%s [%s/%s]: ", prompt, yes, no)
	for {
		fmt.Print(prompt)
		// Read the answer.
		var answer string
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case answer = <-readLine:
		}
		// Parse answer.
		answer = strings.ToLower(answer)
		if answer == "" {
			return defaultValue, nil
		}
		if answer == "y" || answer == "yes" {
			return true, nil
		}
		if answer == "n" || answer == "no" {
			return false, nil
		}
		// No valid answer was provided. Repeat.
	}
}

// Request prompt the user with the given one for an input.
//
// The provided prompt should be in the format of "Enter xyz". RequestInput will
// append colons.
func (input *Stdin) Request(ctx context.Context, prompt string, allowEmpty bool) (string, error) {
	for {
		fmt.Print(fmt.Sprintf("%s: ", prompt))
		var answer string
		select {
		case <-ctx.Done():
			return "", meh.ApplyCode(ctx.Err(), meh.ErrInternal)
		case answer = <-readLine:
		}
		if answer != "" || allowEmpty {
			return answer, nil
		}
		fmt.Println("Please provide a non-empty value.")
	}
}

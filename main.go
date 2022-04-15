package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
	"trollfish-lichess/api"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := api.StreamBots(); err != nil {
			log.Printf("ERR: %v", err)
		}
	}()

	input := make(chan string, 512)
	output := make(chan string, 512)

	if err := startTrollFish(ctx, input, output); err != nil {
		log.Fatal(err)
	}

	listener := New(input, output)

	if err := listener.Events(); err != nil {
		log.Fatal(err)
	}
}

func startTrollFish(ctx context.Context, input <-chan string, output chan<- string) error {
	binary := "/home/jud/projects/trollfish/trollfish"
	dir := "/home/jud/projects/trollfish"

	cmd := exec.CommandContext(ctx, binary)
	cmd.Dir = dir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		log.Fatalf("cmd.Start: %v\n", err)
	}

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for {
			select {
			case line := <-input:
				//fmt.Printf("-> %s\n", line)
				_, err := stdin.Write([]byte(fmt.Sprintf("%s\n", line)))
				if err != nil {
					log.Fatalf("stdin.Write ERR: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// stderr loop
	go func() {
		defer wg.Done()
		r := bufio.NewScanner(stderr)
		for r.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
				line := r.Text()
				log.Printf(fmt.Sprintf("SF STDERR: %s\n", line))
			}
		}
		if err := r.Err(); err != nil {
			log.Printf(fmt.Sprintf("SF ERR: stderr: %v\n", err))
		}
	}()

	// stdout loop
	go func() {
		defer wg.Done()
		r := bufio.NewScanner(stdout)
		for r.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := r.Text()
			if strings.HasPrefix(line, "info string") {
				fmt.Printf("%s <- %s\n", ts(), line)
			}
			output <- line
		}
		if err := r.Err(); err != nil {
			log.Printf(fmt.Sprintf("ERR: stdout: %v\n", err))
		}
	}()

	go func() {
		if err := cmd.Wait(); err != nil {
			log.Fatal(fmt.Sprintf("ERR: %v\n", err))
		}
	}()

	return nil
}

func ts() string {
	return fmt.Sprintf("[%s]", time.Now().Format("2006-01-02 15:04:05.000"))
}

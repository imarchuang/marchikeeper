package main

import (
	"fmt"
	"log"
	"net/http/httptest"
	"time"

	"github.com/marchi/marchikeeper/internal/httpserver"
	"github.com/marchi/marchikeeper/internal/latch"
	"github.com/marchi/marchikeeper/internal/znodes"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	store := znodes.New()
	srv := httptest.NewServer(httpserver.New(store))
	defer srv.Close()

	a := store.CreateSession(5 * time.Second)
	b := store.CreateSession(5 * time.Second)
	pathA, err := latch.Offer(store, a, "/election", "n")
	if err != nil {
		return err
	}
	pathB, err := latch.Offer(store, b, "/election", "n")
	if err != nil {
		return err
	}
	fmt.Printf("A offered %s  B offered %s  (http %s)\n", pathA, pathB, srv.URL)

	ok, _, err := latch.IsLeader(store, pathA)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("A expected leader")
	}
	fmt.Println("leader: A")

	done := make(chan error, 1)
	go func() { done <- latch.WaitUntilLeader(store, b, pathB, 3*time.Second) }()
	time.Sleep(20 * time.Millisecond)
	fmt.Println("closing A session (ephemeral node dies)")
	if err := store.CloseSession(a); err != nil {
		return err
	}
	if err := <-done; err != nil {
		return err
	}
	fmt.Println("leader: B")
	return nil
}

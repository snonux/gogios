package internal

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// Query all federated endpoints and merge states
func mergeFederated(ctx context.Context, state state, conf config) state {
	critical := func(cs checkResult, err error) {
		cs.output = err.Error()
		cs.status = nagiosCritical
		state.update(cs)
	}

	for _, endpoint := range conf.Federated {
		log.Println("Querying federated endpoint", endpoint)
		cs := checkResult{
			name:      fmt.Sprintf("Federated endpoint %s", endpoint),
			epoch:     time.Now().Unix(),
			federated: true,
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			critical(cs, err)
			continue
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			critical(cs, err)
			continue
		}

		bytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			critical(cs, err)
			continue
		}

		if err := state.mergeFromBytes(bytes); err != nil {
			critical(cs, err)
			continue
		}

		cs.output = fmt.Sprintf("OK: Federated endpoint returned %d bytes", len(bytes))
		cs.status = nagiosOk
		state.update(cs)
	}

	log.Println(state)
	return state
}

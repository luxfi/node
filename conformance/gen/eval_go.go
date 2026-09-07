// SPDX-License-Identifier: BSD-3-Clause-Eco
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type GoCorpusVector struct {
	ID             string `json:"id"`
	Chain          string `json:"chain"`
	ExpectedAction string `json:"expected_action"`
	GoExpectation  struct {
		Valid          bool   `json:"valid"`
		TxType         string `json:"tx_type"`
		Status         string `json:"status"`
		Reason         string `json:"reason"`
		RequiresReject bool   `json:"requires_reject"`
	} `json:"go_expectation"`
}

type GoCorpus struct {
	Vectors []GoCorpusVector `json:"vectors"`
}

func main() {
	corpusPath := filepath.Join("..", "corpus", "chain_differential.json")
	if len(os.Args) > 1 {
		corpusPath = os.Args[1]
	}

	data, err := os.ReadFile(corpusPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read corpus: %v\n", err)
		os.Exit(1)
	}

	var corpus GoCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse corpus: %v\n", err)
		os.Exit(1)
	}

	for _, v := range corpus.Vectors {
		status := v.GoExpectation.Status
		if status == "" {
			if v.GoExpectation.RequiresReject || !v.GoExpectation.Valid {
				status = "REJECTED"
			} else {
				status = "ACCEPTED"
			}
		}
		detail := fmt.Sprintf("type=%s valid=%v", v.GoExpectation.TxType, v.GoExpectation.Valid)
		if v.GoExpectation.Reason != "" {
			detail += " reason=" + v.GoExpectation.Reason
		}
		fmt.Printf("RESULT id=%s status=%s detail=%s\n", v.ID, status, detail)
	}
}

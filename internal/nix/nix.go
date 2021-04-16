// SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
//
// SPDX-License-Identifier: AGPL-3.0-only

// The nix package provides an interface to call and parse the output
// of Nix commands.
package nix

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// runNixCommand runs an arbitrary Nix command, and deserializes its
// JSON output.
func runNixCommand(result interface{}, args ...string) error {
	cmd := exec.Command("nix", args...)
	out, err := cmd.Output()
	if err != nil {
		return &NixCommandError{cmd: cmd, error: err}
	}

	if err := json.Unmarshal(out, result); err != nil {
		return &NixCommandError{cmd: cmd, error: err}
	}

	return nil
}

// NarHash returns a cryptographic hash of the NAR serialization of a
// Nix store path.
func NarHash(storePath string) (string, error) {
	var parsedOut []struct {
		NarHash string `json:"narHash"`
	}
	if err := runNixCommand(&parsedOut, "path-info", "--json", storePath); err != nil {
		return "", err
	}
	return parsedOut[0].NarHash, nil
}

// Build builds a Nix flake or other installable, and returns the
// output path of the built derivation.
func Build(installable string) (string, error) {
	var parsedOut []struct {
		Outputs struct {
			Out string `json:"out"`
		} `json:"outputs"`
	}
	if err := runNixCommand(&parsedOut, "build", "--json", "--no-link", installable); err != nil {
		return "", err
	}
	return parsedOut[0].Outputs.Out, nil
}

// A NixCommandError is returned when running a Nix command fails.
type NixCommandError struct {
	cmd *exec.Cmd
	error
}

func (e *NixCommandError) Error() string {
	return fmt.Sprintf("snowweb: running command `%v`: %v", e.cmd, e.error)
}

func (e *NixCommandError) Unwrap() error {
	return e.error
}

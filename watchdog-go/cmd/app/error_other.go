// SPDX-License-Identifier: Apache-2.0
//go:build !windows

package main

func showError(message string) { /* Other platforms report startup errors through launcher.log. */ }

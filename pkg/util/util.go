//go:build freebsd || linux || darwin
// +build freebsd linux darwin

/*
Copyright 2017 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"errors"

	corev1 "k8s.io/api/core/v1"
)

func CPUInCPUList(cpu uint, cpuList []uint) bool {
	for _, cpuListID := range cpuList {
		if cpuListID == cpu {
			return true
		}
	}

	return false
}

func NodeNameInNodeList(name string, nodeList []corev1.Node) bool {
	for _, node := range nodeList {
		if node.Name == name {
			return true
		}
	}

	return false
}

func StringInStringList(item string, itemList []string) bool {
	for _, i := range itemList {
		if i == item {
			return true
		}
	}

	return false
}

// UnpackErrsToStrings will try to unpack a multi-error to a list of strings, if possible, if not it will return string
// representation as a first element
func UnpackErrsToStrings(err error) *[]string {
	if err == nil {
		return &[]string{}
	}

	var joinedErr interface{ Unwrap() []error }
	if errors.As(err, &joinedErr) {
		errs := joinedErr.Unwrap()
		stringErrs := make([]string, len(errs))
		for i, individualErr := range errs {
			stringErrs[i] = individualErr.Error()
		}
		return &stringErrs
	}
	return &[]string{err.Error()}
}

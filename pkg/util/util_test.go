/*
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
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type cpuTest struct {
	name     uint
	isInList bool
}

type nodeTest struct {
	nodeName string
	isInList bool
}

type stringTest struct {
	str      string
	isInList bool
}

func TestCPUInCPUList(t *testing.T) {

	cpuList := []uint{1222267, 811198, 0}

	testCases := []cpuTest{

		{
			name:     1222267,
			isInList: true,
		},
		{
			name:     44,
			isInList: false,
		},
	}

	var nilList []uint = nil

	for _, want := range testCases {

		resultTrue := CPUInCPUList(want.name, cpuList)
		assert.Equal(t, want.isInList, resultTrue, "One variable that is expected to be in the List or not be in the list was handeled incorrectly")

		resultFalse := CPUInCPUList(want.name, nilList)
		assert.Equal(t, false, resultFalse, "nilList should be null")
	}

}

func TestNodeNameInNodeList(t *testing.T) {
	nodeList := []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "Node1",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "Node2",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "!",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "",
			},
		},
	}
	var nilList []corev1.Node = nil
	testCases := []nodeTest{
		{
			nodeName: "Node1",
			isInList: true,
		},
		{
			nodeName: "",
			isInList: true,
		},
		{
			nodeName: "?",
			isInList: false,
		},
		{
			nodeName: "node10",
			isInList: false,
		},
	}

	for _, want := range testCases {

		resultTrue := NodeNameInNodeList(want.nodeName, nodeList)
		assert.Equal(t, want.isInList, resultTrue, "One variable that is expected to be in the list or not be in the list was handeled incorrectly")

		resultFalse := NodeNameInNodeList(want.nodeName, nilList)
		assert.Equal(t, false, resultFalse, "nilList should be empty")
	}

}

func TestStringInStringList(t *testing.T) {
	itemList := []string{"aa", "b", "3", "5", "", "!"}
	var nilList []string = nil

	testCases := []stringTest{
		{
			str:      "aa",
			isInList: true,
		},
		{
			str:      "",
			isInList: true,
		},
		{
			str:      "!",
			isInList: true,
		},
		{
			str:      "hi",
			isInList: false,
		},
		{
			str:      "-",
			isInList: false,
		},
	}
	for _, want := range testCases {

		resultTrue := StringInStringList(want.str, itemList)
		assert.Equal(t, want.isInList, resultTrue, "One variable that is expected to be in the list or not be in the list was handeled incorrectly")

		resultFalse := StringInStringList(want.str, nilList)
		assert.Equal(t, false, resultFalse, "nilList should be empty")
	}

}

func TestUnpackErrsToStrings(t *testing.T) {
	assert.Equal(t, &[]string{}, UnpackErrsToStrings(nil))

	// single error
	const errString1 = "err1"
	assert.Equal(t, &[]string{errString1}, UnpackErrsToStrings(errors.New(errString1)))

	// wrapped err
	const errString2 = "err2"
	assert.Equal(
		t,
		&[]string{errString1, errString2},
		UnpackErrsToStrings(errors.Join(errors.New(errString1), errors.New(errString2))),
	)

}

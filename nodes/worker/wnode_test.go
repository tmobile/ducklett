/*
Copyright 2020 The Kubernetes Authors.

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
package worker

//	apiv1 "k8s.io/api/core/v1"
//	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

var (
	handler = NewNodeHandler(nil, "test", "test", "test", nil, 3)
)

/* func TestAtMaxConcurrentUpdates(t *testing.T) {
	tests := []struct {
		inUpdating int
		inMax      int
		name       string
		want       bool
	}{
		{
			name:       "test with too many updating",
			inUpdating: 1,
			inMax:      1,
			want:       true,
		},
		{
			name:       "test with too many updating",
			inUpdating: 2,
			inMax:      1,
			want:       true,
		},
		{
			name:       "test with too many updating",
			inUpdating: 3,
			inMax:      3,
			want:       true,
		},
		{
			name:       "test with not at max updating",
			inUpdating: 2,
			inMax:      3,
			want:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := handler.AtMaxConcurrentUpdates(tc.inUpdating, tc.inMax, 0)
			require.Equal(t, tc.want, got)
		})
	}
} */

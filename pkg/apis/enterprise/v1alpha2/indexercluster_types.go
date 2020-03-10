// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// default all fields to being optional
// +kubebuilder:validation:Optional

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
// see also https://book.kubebuilder.io/reference/markers/crd.html

// IndexerClusterSpec defines the desired state of a Splunk Enterprise indexer cluster
type IndexerClusterSpec struct {
	CommonSplunkSpec `json:",inline"`

	// Number of search head pods; a search head cluster will be created if > 1
	Replicas int32 `json:"replicas"`
}

// IndexerClusterStatus defines the observed state of a Splunk Enterprise indexer cluster
type IndexerClusterStatus struct {
	// current phase of the indexer cluster
	Phase ResourcePhase `json:"phase"`

	// current phase of the cluster master
	ClusterMasterPhase ResourcePhase `json:"clusterMasterPhase"`

	// desired number of indexer peers
	Replicas int32 `json:"replicas"`

	// current number of ready indexer peers
	ReadyReplicas int32 `json:"readyReplicas"`

	// selector for pods, used by HorizontalPodAutoscaler
	Selector string `json:"selector"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// IndexerCluster is the Schema for a Splunk Enterprise indexer cluster
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=indexerclusters,scope=Namespaced,shortName=idc;idxc
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of indexer cluster"
// +kubebuilder:printcolumn:name="Master",type="string",JSONPath=".status.clusterMasterPhase",description="Status of cluster master"
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".status.replicas",description="Desired number of indexer peers"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas",description="Current number of ready indexer peers"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of indexer cluster"
type IndexerCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IndexerClusterSpec   `json:"spec,omitempty"`
	Status IndexerClusterStatus `json:"status,omitempty"`
}

// GetIdentifier is a convenience function to return unique identifier for the Splunk enterprise deployment
func (cr *IndexerCluster) GetIdentifier() string {
	return cr.ObjectMeta.Name
}

// GetNamespace is a convenience function to return namespace for a Splunk enterprise deployment
func (cr *IndexerCluster) GetNamespace() string {
	return cr.ObjectMeta.Namespace
}

// GetTypeMeta is a convenience function to return a TypeMeta object
func (cr *IndexerCluster) GetTypeMeta() metav1.TypeMeta {
	return cr.TypeMeta
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// IndexerClusterList contains a list of IndexerCluster
type IndexerClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IndexerCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IndexerCluster{}, &IndexerClusterList{})
}
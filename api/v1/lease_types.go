/*
Copyright 2024.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// LeaseSpec defines the desired state of Lease
type LeaseSpec struct {
	// holderIdentity contains the identity of the holder of a current lease.
	HolderIdentity string `json:"holderIdentity" protobuf:"bytes,1,opt,name=holderIdentity"`
	// leaseDurationSeconds is a duration that candidates for a lease need
	// to wait to force acquire it. This is measure against time of last
	// observed renewTime.
	LeaseDurationSeconds int32 `json:"leaseDurationSeconds" protobuf:"varint,2,opt,name=leaseDurationSeconds"`
	// acquireTime is a time when the current lease was acquired.
	AcquireTime metav1.MicroTime `json:"acquireTime" protobuf:"bytes,3,opt,name=acquireTime"`
	// renewTime is a time when the current holder of a lease has last
	// updated the lease.
	RenewTime metav1.MicroTime `json:"renewTime" protobuf:"bytes,4,opt,name=renewTime"`
}

// LeaseStatus defines the observed state of Lease
type LeaseStatus struct {
	// observedholderIdentity contains the identity of the holder of a current lease.
	ObservedHolderIdentity string `json:"observedHolderIdentity" protobuf:"bytes,1,opt,name=observedHolderIdentity"`
	// observedAcquireTime is a time when the current lease was acquired.
	ObservedAcquireTime metav1.MicroTime `json:"observedAcquireTime" protobuf:"bytes,3,opt,name=observedAcquireTime"`
	// leaseTransitions is the number of transitions of a lease between
	// holders.
	LeaseTransitions int32 `json:"leaseTransitions" protobuf:"varint,5,opt,name=leaseTransitions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Lease is the Schema for the leases API
type Lease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LeaseSpec   `json:"spec,omitempty"`
	Status LeaseStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// LeaseList contains a list of Lease
type LeaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Lease `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Lease{}, &LeaseList{})
}

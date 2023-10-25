/*
Copyright 2023.

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

package v1beta1

import (
	opcuaserver "github.com/paolerm/orca-opcua-server/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Cluster struct {
	Id string `json:"id"`
}

type OpcuaOverrides struct {
	ServerCount              int                     `json:"serverCount,omitempty"`
	AssetPerServer           int                     `json:"assetPerServer,omitempty"`
	TagCount                 int                     `json:"tagCount,omitempty"`
	AssetUpdateRatePerSecond int                     `json:"assetUpdateRatePerSecond,omitempty"`
	ChangeRateMs 			 int 					 `json:"changeRateMs,omitempty"`
	SamplingIntervalMs 		 int 					 `json:"samplingInterval,omitempty"`
	DockerImage              opcuaserver.DockerImage `json:"dockerImage,omitempty"`
	LogLevel 				 string 				 `json:"logLevel,omitempty"`
	OpcuaServerLogLevel 	 string 				 `json:"opcuaServerLogLevel,omitempty"`

}

type ScenarioOvverides struct {
	OpcuaOverrides []OpcuaOverrides `json:"opcuaOverrides,omitempty"`
}

type ScenarioDefinition struct {
	TemplateId string            `json:"templateId"`
	Overrides  ScenarioOvverides `json:"overrides,omitempty"`
}

// ScenarioSpec defines the desired state of Scenario
type ScenarioSpec struct {
	Cluster            Cluster            `json:"cluster"`
	ScenarioDefinition ScenarioDefinition `json:"scenarioDefinition"`
}

// ScenarioStatus defines the observed state of Scenario
type ScenarioStatus struct {
	// TODO: current scenario running

	// TODO: public IP addresses for each OPCUA
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Scenario is the Schema for the scenarios API
type Scenario struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ScenarioSpec   `json:"spec,omitempty"`
	Status ScenarioStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ScenarioList contains a list of Scenario
type ScenarioList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Scenario `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Scenario{}, &ScenarioList{})
}

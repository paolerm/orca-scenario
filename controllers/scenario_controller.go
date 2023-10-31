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

package controllers

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strconv"

	mqttclient "github.com/paolerm/orca-mqtt-client/api/v1beta1"
	opcuaserver "github.com/paolerm/orca-opcua-server/api/v1beta1"
	scenariotemplate "github.com/paolerm/orca-scenario-template/api/v1beta1"

	orcav1beta1 "github.com/paolerm/orca-scenario/api/v1beta1"
)

// ScenarioReconciler reconciles a Scenario object
type ScenarioReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const scenarioFinalizer = "paermini.com/scenario-finalizer"
const scenarioTemplateNamespace = "scenario-template-catalog"

//+kubebuilder:rbac:groups=orca.paermini.com,resources=scenarios,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=orca.paermini.com,resources=scenarios/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=orca.paermini.com,resources=scenarios/finalizers,verbs=update
//+kubebuilder:rbac:groups=orca.paermini.com,resources=scenariotemplates,verbs=get;list;watch
//+kubebuilder:rbac:groups=orca.paermini.com,resources=scenariotemplates/status,verbs=get
//+kubebuilder:rbac:groups=orca.paermini.com,resources=mqttclients,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=orca.paermini.com,resources=mqttclients/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=orca.paermini.com,resources=opcuaservers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=orca.paermini.com,resources=opcuaservers/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Scenario object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *ScenarioReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	scenario := &orcav1beta1.Scenario{}
	err := r.Get(ctx, req.NamespacedName, scenario)
	if err != nil {
		return ctrl.Result{}, err
	}

	isCrDeleted := scenario.GetDeletionTimestamp() != nil
	if isCrDeleted {
		if controllerutil.ContainsFinalizer(scenario, scenarioFinalizer) {
			// Run finalization logic. If the
			// finalization logic fails, don't remove the finalizer so
			// that we can retry during the next reconciliation.
			if err := r.finalizeScenario(ctx, req, scenario); err != nil {
				return ctrl.Result{}, err
			}

			// Remove scenarioFinalizer. Once all finalizers have been
			// removed, the object will be deleted.
			controllerutil.RemoveFinalizer(scenario, scenarioFinalizer)
			err := r.Update(ctx, scenario)
			if err != nil {
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	logger.Info("Getting scenario Template under namespace '" + scenarioTemplateNamespace + "' and ID '" + scenario.Spec.ScenarioDefinition.TemplateId + "'...")

	scenarioTemplate := &scenariotemplate.ScenarioTemplate{}
	err = r.Get(ctx, client.ObjectKey{
		Namespace: scenarioTemplateNamespace,
		Name:      scenario.Spec.ScenarioDefinition.TemplateId}, scenarioTemplate)
	if err != nil {
		return ctrl.Result{}, err
	}

	scenario.Status.OpcuaServerCr = []string{}
	scenario.Status.MqttClientCr = []string{}

	for i := 0; i < len(scenarioTemplate.Spec.OpcuaSiteSpec); i++ {
		opcuaSpec := scenarioTemplate.Spec.OpcuaSiteSpec[i]
		opcuaName := scenario.Spec.Cluster.Id + "-" + scenario.Spec.ScenarioDefinition.TemplateId + "-" + opcuaSpec.Id
		opcuaNamespace := req.NamespacedName.Namespace

		// apply overrides
		overrideIndex := -1
		if scenario.Spec.ScenarioDefinition.Overrides.OpcuaOverrides != nil {
			overrideIndex = ContainsOpcuaOverride(scenario.Spec.ScenarioDefinition.Overrides.OpcuaOverrides, opcuaSpec.Id)
			logger.Info("Override index for scenario " + opcuaSpec.Id + " is equals to " + strconv.Itoa(overrideIndex))
			if overrideIndex > -1 {
				opcuaSpec = ApplyOpcuaOverrides(opcuaSpec, scenario.Spec.ScenarioDefinition.Overrides.OpcuaOverrides[overrideIndex])
			}
		}

		// update Id to include clusterId and templateId into the OPCUA CR name
		opcuaSpec.Id = opcuaName
		opcuaNamepsacedName := types.NamespacedName{
			Name:      opcuaName,
			Namespace: opcuaNamespace,
		}

		logger.Info("Getting opcua-server CR under namespace " + opcuaNamespace + " and name " + opcuaName + "...")
		existingOpcuaServer := &opcuaserver.OpcuaServer{}
		err = r.Get(ctx, opcuaNamepsacedName, existingOpcuaServer)
		if err != nil {
			logger.Info("Creating opcua-server CR under namespace " + opcuaNamespace + " and name " + opcuaName + "...")

			opcuaServer := &opcuaserver.OpcuaServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      opcuaName,
					Namespace: opcuaNamespace,
					Labels: map[string]string{
						"simulation": scenario.Spec.Cluster.Id + "-" + scenario.Spec.ScenarioDefinition.TemplateId,
					},
				},
				Spec: opcuaserver.OpcuaServerSpec{
					Id:                       opcuaName,
					ServerCount:              opcuaSpec.ServerCount,
					AssetPerServer:           opcuaSpec.AssetPerServer,
					TagCount:                 opcuaSpec.TagCount,
					AssetUpdateRatePerSecond: opcuaSpec.AssetUpdateRatePerSecond,
					SamplingIntervalMs:       opcuaSpec.SamplingIntervalMs,
					DockerImageId:            opcuaSpec.DockerImageId,
				},
			}

			err := r.Create(ctx, opcuaServer)
			if err != nil {
				logger.Error(err, "Failed to create opcua-server CR!")
				return ctrl.Result{}, err
			}
		} else {
			logger.Info("Updating opcua-server CR under namespace " + opcuaNamespace + " and name " + opcuaName + "...")

			existingOpcuaServer.Spec = opcuaSpec

			err := r.Update(ctx, existingOpcuaServer)
			if err != nil {
				logger.Error(err, "Failed to update opcua-server CR!")
				return ctrl.Result{}, err
			}
		}

		scenario.Status.OpcuaServerCr = append(scenario.Status.OpcuaServerCr, opcuaName)
	}

	for i := 0; i < len(scenarioTemplate.Spec.MqttClientSiteSpec); i++ {
		mqttClientSpec := scenarioTemplate.Spec.MqttClientSiteSpec[i]
		mqttClientName := scenario.Spec.Cluster.Id + "-" + scenario.Spec.ScenarioDefinition.TemplateId + "-" + mqttClientSpec.Id
		mqttClientNamespace := req.NamespacedName.Namespace

		// apply overrides
		overrideIndex := -1
		if scenario.Spec.ScenarioDefinition.Overrides.MqttClientOverrides != nil {
			logger.Info("Looking for overrides. Spec ID: " + mqttClientSpec.Id + ". MqttClientOverrides size: " + strconv.Itoa(len(scenario.Spec.ScenarioDefinition.Overrides.MqttClientOverrides)))
			overrideIndex = ContainsMqttClientOverride(scenario.Spec.ScenarioDefinition.Overrides.MqttClientOverrides, mqttClientSpec.Id)
			logger.Info("OverrideIndex: " + strconv.Itoa(overrideIndex))
			if overrideIndex > -1 {
				mqttClientSpec = ApplyMqttClientOverrides(mqttClientSpec, scenario.Spec.ScenarioDefinition.Overrides.MqttClientOverrides[overrideIndex])
			}
		}

		mqttClientSpec.Id = mqttClientName
		mqttClientNamepsacedName := types.NamespacedName{
			Name:      mqttClientName,
			Namespace: mqttClientNamespace,
		}

		logger.Info("Getting MQTT client CR under namespace " + mqttClientNamespace + " and name " + mqttClientName + "...")
		existingMqttClient := &mqttclient.MqttClient{}
		err = r.Get(ctx, mqttClientNamepsacedName, existingMqttClient)
		if err != nil {
			logger.Info("Creating MQTT client CR under namespace " + mqttClientNamespace + " and name " + mqttClientName + "...")

			mqttClient := &mqttclient.MqttClient{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mqttClientName,
					Namespace: mqttClientNamespace,
					Labels: map[string]string{
						"simulation": scenario.Spec.Cluster.Id + "-" + scenario.Spec.ScenarioDefinition.TemplateId,
					},
				},
				Spec: mqttclient.MqttClientSpec{
					Id:                       mqttClientName,
					HostName:                 mqttClientSpec.HostName,
					Port:                     mqttClientSpec.Port,
					ConnectionLimitPerSecond: mqttClientSpec.ConnectionLimitPerSecond,
					SendingLimitPerSecond:    mqttClientSpec.SendingLimitPerSecond,
					Protocol:                 mqttClientSpec.Protocol,
					EnableTls:                mqttClientSpec.EnableTls,
					ClientConfigs:            mqttClientSpec.ClientConfigs,
				},
			}

			err := r.Create(ctx, mqttClient)
			if err != nil {
				logger.Error(err, "Failed to create MQTT client CR!")
				return ctrl.Result{}, err
			}
		} else {
			logger.Info("Updating MQTT client CR under namespace " + mqttClientNamespace + " and name " + mqttClientName + "...")

			existingMqttClient.Spec = mqttClientSpec

			err := r.Update(ctx, existingMqttClient)
			if err != nil {
				logger.Error(err, "Failed to update MQTT client CR!")
				return ctrl.Result{}, err
			}
		}

		scenario.Status.MqttClientCr = append(scenario.Status.MqttClientCr, mqttClientName)
	}

	// TODO: remove extra

	// update status field
	err = r.Status().Update(ctx, scenario)
	if err != nil {
		logger.Error(err, "Failed to update CR!")
		return ctrl.Result{}, err
	}

	// Add finalizer for this CR
	if !controllerutil.ContainsFinalizer(scenario, scenarioFinalizer) {
		logger.Info("Adding finalize to scenario CR...")
		controllerutil.AddFinalizer(scenario, scenarioFinalizer)
		err = r.Update(ctx, scenario)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScenarioReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&orcav1beta1.Scenario{}).
		Complete(r)
}

func (r *ScenarioReconciler) finalizeScenario(ctx context.Context, req ctrl.Request, scenario *orcav1beta1.Scenario) error {
	logger := log.FromContext(ctx)

	// Cleanup OPCUA servers
	logger.Info("Getting all OPCUA servers...")
	opcuaServerList := &opcuaserver.OpcuaServerList{}
	opts := []client.ListOption{
		client.InNamespace(req.NamespacedName.Namespace),
		client.MatchingLabels{"simulation": scenario.Spec.Cluster.Id + "-" + scenario.Spec.ScenarioDefinition.TemplateId},
	}

	err := r.List(ctx, opcuaServerList, opts...)
	if err != nil {
		logger.Error(err, "Failed to get opcuaServer list!")
		return err
	}

	logger.Info("Found " + strconv.Itoa(len(opcuaServerList.Items)) + " OPCUA servers.")
	for i := 0; i < len(opcuaServerList.Items); i++ {
		opcuaToDelete := opcuaServerList.Items[i]
		logger.Info("Deleting " + opcuaToDelete.ObjectMeta.Name + " OpcuaServerOperator CR...")

		err = r.Delete(ctx, &opcuaToDelete)
		if err != nil {
			logger.Error(err, "Failed to delete OpcuaServerOperator CR!")
			return err
		}
	}

	// Cleanup MQTT clients
	logger.Info("Getting all MQTT clients...")
	mqttClientList := &mqttclient.MqttClientList{}
	err = r.List(ctx, mqttClientList, opts...)
	if err != nil {
		logger.Error(err, "Failed to get MQTT client list!")
		return err
	}

	logger.Info("Found " + strconv.Itoa(len(mqttClientList.Items)) + " MQTT clients.")
	for i := 0; i < len(mqttClientList.Items); i++ {
		mqttClientToDelete := mqttClientList.Items[i]
		logger.Info("Deleting " + mqttClientToDelete.ObjectMeta.Name + " MqttClientOperator CR...")

		err = r.Delete(ctx, &mqttClientToDelete)
		if err != nil {
			logger.Error(err, "Failed to delete MqttClientOperator CR!")
			return err
		}
	}

	logger.Info("Successfully finalized")
	return nil
}

func ContainsOpcuaOverride(s []orcav1beta1.OpcuaOverrides, id string) int {
	for i, a := range s {
		if a.Id == id {
			return i
		}
	}
	return -1
}

func ContainsMqttClientOverride(s []orcav1beta1.MqttClientOverrides, id string) int {
	for i, a := range s {
		if a.Id == id {
			return i
		}
	}
	return -1
}

func ApplyOpcuaOverrides(spec opcuaserver.OpcuaServerSpec, overrides orcav1beta1.OpcuaOverrides) opcuaserver.OpcuaServerSpec {
	result := spec

	// TODO: use reflection instead?

	// TODO: we should override default value to -1, otherwise there is no way to shut down all servers
	if overrides.ServerCount != 0 {
		result.ServerCount = overrides.ServerCount
	}
	if overrides.AssetPerServer != 0 {
		result.AssetPerServer = overrides.AssetPerServer
	}
	if overrides.TagCount != 0 {
		result.TagCount = overrides.TagCount
	}
	if overrides.AssetUpdateRatePerSecond != 0 {
		result.AssetUpdateRatePerSecond = overrides.AssetUpdateRatePerSecond
	}
	if overrides.ChangeRateMs != 0 {
		result.ChangeRateMs = overrides.ChangeRateMs
	}
	if overrides.SamplingIntervalMs != 0 {
		result.SamplingIntervalMs = overrides.SamplingIntervalMs
	}
	if overrides.DockerImageId != "" {
		result.DockerImageId = overrides.DockerImageId
	}
	if overrides.LogLevel != "" {
		result.LogLevel = overrides.LogLevel
	}
	if overrides.OpcuaServerLogLevel != "" {
		result.OpcuaServerLogLevel = overrides.OpcuaServerLogLevel
	}

	return result
}

func ApplyMqttClientOverrides(spec mqttclient.MqttClientSpec, overrides orcav1beta1.MqttClientOverrides) mqttclient.MqttClientSpec {
	result := spec

	// TODO: use reflection instead?

	if overrides.HostName != "" {
		result.HostName = overrides.HostName
	}
	if overrides.Port != 0 {
		result.Port = overrides.Port
	}
	if overrides.ConnectionLimitPerSecond != 0 {
		result.ConnectionLimitPerSecond = overrides.ConnectionLimitPerSecond
	}
	if overrides.SendingLimitPerSecond != 0 {
		result.SendingLimitPerSecond = overrides.SendingLimitPerSecond
	}
	if overrides.Protocol != "" {
		result.Protocol = overrides.Protocol
	}

	// TODO: override EnableTls?

	// TODO: override ClientConfigs?

	return result
}

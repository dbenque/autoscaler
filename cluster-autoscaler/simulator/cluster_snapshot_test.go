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

package simulator

import (
	"fmt"
	"testing"

	. "k8s.io/autoscaler/cluster-autoscaler/utils/test"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	schedulernodeinfo "k8s.io/kubernetes/pkg/scheduler/nodeinfo"

	"github.com/stretchr/testify/assert"
)

var snapshots = map[string]func() ClusterSnapshot{
	"basic": func() ClusterSnapshot { return NewBasicClusterSnapshot() },
	"delta": func() ClusterSnapshot { return NewDeltaClusterSnapshot() },
}

func nodeNames(nodes []*apiv1.Node) []string {
	names := make([]string, len(nodes), len(nodes))
	for i, node := range nodes {
		names[i] = node.Name
	}
	return names
}

func nodeInfoNames(nodeInfos []*schedulernodeinfo.NodeInfo) []string {
	names := make([]string, len(nodeInfos), len(nodeInfos))
	for i, node := range nodeInfos {
		names[i] = node.Node().Name
	}
	return names
}

func nodeInfoPods(nodeInfos []*schedulernodeinfo.NodeInfo) []*apiv1.Pod {
	pods := []*apiv1.Pod{}
	for _, node := range nodeInfos {
		pods = append(pods, node.Pods()...)
	}
	return pods
}

func TestForkAddNode(t *testing.T) {
	nodeCount := 3

	nodes := createTestNodes(nodeCount)
	extraNodes := createTestNodesWithPrefix("tmp", 2)

	for name, snapshotFactory := range snapshots {
		t.Run(fmt.Sprintf("%s: fork should not affect base data: adding nodes", name),
			func(t *testing.T) {
				clusterSnapshot := snapshotFactory()
				err := clusterSnapshot.AddNodes(nodes)
				assert.NoError(t, err)

				err = clusterSnapshot.Fork()
				assert.NoError(t, err)

				for _, node := range extraNodes {
					err = clusterSnapshot.AddNode(node)
					assert.NoError(t, err)
				}
				forkNodes, err := clusterSnapshot.NodeInfos().List()
				assert.NoError(t, err)
				assert.ElementsMatch(t, append(nodeNames(nodes), nodeNames(extraNodes)...), nodeInfoNames(forkNodes))

				err = clusterSnapshot.Revert()
				assert.NoError(t, err)

				baseNodes, err := clusterSnapshot.NodeInfos().List()
				assert.NoError(t, err)
				assert.ElementsMatch(t, nodeNames(nodes), nodeInfoNames(baseNodes))
			})
	}
}

func TestForkAddPods(t *testing.T) {
	nodeCount := 3
	podCount := 90

	nodes := createTestNodes(nodeCount)
	pods := createTestPods(podCount)
	assignPodsToNodes(pods, nodes)

	for name, snapshotFactory := range snapshots {
		t.Run(fmt.Sprintf("%s: fork should not affect base data: adding pods", name),
			func(t *testing.T) {
				clusterSnapshot := snapshotFactory()
				err := clusterSnapshot.AddNodes(nodes)
				assert.NoError(t, err)

				err = clusterSnapshot.Fork()
				assert.NoError(t, err)

				for _, pod := range pods {
					err = clusterSnapshot.AddPod(pod, pod.Spec.NodeName)
					assert.NoError(t, err)
				}
				forkPods, err := clusterSnapshot.Pods().List(labels.Everything())
				assert.NoError(t, err)
				assert.ElementsMatch(t, pods, forkPods)
				forkNodes, err := clusterSnapshot.NodeInfos().List()
				assert.NoError(t, err)
				assert.ElementsMatch(t, nodeNames(nodes), nodeInfoNames(forkNodes))

				err = clusterSnapshot.Revert()
				assert.NoError(t, err)

				basePods, err := clusterSnapshot.Pods().List(labels.Everything())
				assert.NoError(t, err)
				assert.Equal(t, 0, len(basePods))
				baseNodes, err := clusterSnapshot.NodeInfos().List()
				assert.NoError(t, err)
				assert.ElementsMatch(t, nodeNames(nodes), nodeInfoNames(baseNodes))
			})
	}
}

func TestForkRemovePods(t *testing.T) {
	nodeCount := 3
	podCount := 90
	deletedPodCount := 10

	nodes := createTestNodes(nodeCount)
	pods := createTestPods(podCount)
	assignPodsToNodes(pods, nodes)

	for name, snapshotFactory := range snapshots {
		t.Run(fmt.Sprintf("%s: fork should not affect base data: removing pods", name),
			func(t *testing.T) {
				clusterSnapshot := snapshotFactory()
				err := clusterSnapshot.AddNodes(nodes)
				assert.NoError(t, err)

				for _, pod := range pods {
					err = clusterSnapshot.AddPod(pod, pod.Spec.NodeName)
					assert.NoError(t, err)
				}

				err = clusterSnapshot.Fork()
				assert.NoError(t, err)

				for _, pod := range pods[:deletedPodCount] {
					err = clusterSnapshot.RemovePod(pod.Namespace, pod.Name, pod.Spec.NodeName)
					assert.NoError(t, err)
				}

				forkPods, err := clusterSnapshot.Pods().List(labels.Everything())
				assert.NoError(t, err)
				assert.ElementsMatch(t, pods[deletedPodCount:], forkPods)
				forkNodes, err := clusterSnapshot.NodeInfos().List()
				assert.NoError(t, err)
				assert.ElementsMatch(t, nodeNames(nodes), nodeInfoNames(forkNodes))
				assert.ElementsMatch(t, pods[deletedPodCount:], nodeInfoPods(forkNodes))

				err = clusterSnapshot.Revert()
				assert.NoError(t, err)

				basePods, err := clusterSnapshot.Pods().List(labels.Everything())
				assert.NoError(t, err)
				assert.ElementsMatch(t, pods, basePods)
				baseNodes, err := clusterSnapshot.NodeInfos().List()
				assert.NoError(t, err)
				assert.ElementsMatch(t, nodeNames(nodes), nodeInfoNames(baseNodes))
				assert.ElementsMatch(t, pods, nodeInfoPods(baseNodes))
			})
	}
}

func extractNodes(nodeInfos []*schedulernodeinfo.NodeInfo) []*apiv1.Node {
	nodes := []*apiv1.Node{}
	for _, ni := range nodeInfos {
		nodes = append(nodes, ni.Node())
	}
	return nodes
}

func TestReAddNode(t *testing.T) {
	for name, snapshotFactory := range snapshots {
		t.Run(fmt.Sprintf("%s: re-add node", name),
			func(t *testing.T) {
				snapshot := snapshotFactory()

				node := BuildTestNode("node", 10, 100)
				err := snapshot.AddNode(node)
				assert.NoError(t, err)

				err = snapshot.Fork()
				assert.NoError(t, err)

				err = snapshot.RemoveNode("node")
				assert.NoError(t, err)

				err = snapshot.AddNode(node)
				assert.NoError(t, err)

				forkNodes, err := snapshot.NodeInfos().List()
				assert.NoError(t, err)
				assert.ElementsMatch(t, []*apiv1.Node{node}, extractNodes(forkNodes))

				err = snapshot.Commit()
				committedNodes, err := snapshot.NodeInfos().List()
				assert.NoError(t, err)
				assert.ElementsMatch(t, []*apiv1.Node{node}, extractNodes(committedNodes))
			})
	}
}

type snapshotState struct {
	nodes []*apiv1.Node
	pods  []*apiv1.Pod
}

func compareStates(t *testing.T, a, b snapshotState) {
	assert.ElementsMatch(t, a.nodes, b.nodes)
	assert.ElementsMatch(t, a.pods, b.pods)
}

func getSnapshotState(t *testing.T, snapshot ClusterSnapshot) snapshotState {
	nodes, err := snapshot.NodeInfos().List()
	assert.NoError(t, err)
	pods, err := snapshot.Pods().List(labels.Everything())
	assert.NoError(t, err)
	return snapshotState{extractNodes(nodes), pods}
}

func startSnapshot(t *testing.T, snapshotFactory func() ClusterSnapshot, state snapshotState) ClusterSnapshot {
	snapshot := snapshotFactory()
	err := snapshot.AddNodes(state.nodes)
	assert.NoError(t, err)
	for _, pod := range state.pods {
		err := snapshot.AddPod(pod, pod.Spec.NodeName)
		assert.NoError(t, err)
	}
	return snapshot
}

func TestForking(t *testing.T) {
	node := BuildTestNode("specialNode", 10, 100)
	pod := BuildTestPod("specialPod", 1, 1)
	pod.Spec.NodeName = node.Name

	testCases := []struct {
		name          string
		op            func(ClusterSnapshot)
		state         snapshotState
		modifiedState snapshotState
	}{
		{
			name: "add node",
			op: func(snapshot ClusterSnapshot) {
				err := snapshot.AddNode(node)
				assert.NoError(t, err)
			},
			modifiedState: snapshotState{
				nodes: []*apiv1.Node{node},
			},
		},
		{
			name: "remove node",
			state: snapshotState{
				nodes: []*apiv1.Node{node},
			},
			op: func(snapshot ClusterSnapshot) {
				err := snapshot.RemoveNode(node.Name)
				assert.NoError(t, err)
			},
		},
		{
			name: "add pod, then remove node",
			state: snapshotState{
				nodes: []*apiv1.Node{node},
			},
			op: func(snapshot ClusterSnapshot) {
				err := snapshot.AddPod(pod, node.Name)
				assert.NoError(t, err)
				err = snapshot.RemoveNode(node.Name)
				assert.NoError(t, err)
			},
		},
	}

	for name, snapshotFactory := range snapshots {
		for _, tc := range testCases {
			t.Run(fmt.Sprintf("%s: %s base", name, tc.name), func(t *testing.T) {
				snapshot := startSnapshot(t, snapshotFactory, tc.state)
				tc.op(snapshot)

				// Modifications should be applied.
				compareStates(t, tc.modifiedState, getSnapshotState(t, snapshot))
			})
			t.Run(fmt.Sprintf("%s: %s fork", name, tc.name), func(t *testing.T) {
				snapshot := startSnapshot(t, snapshotFactory, tc.state)

				err := snapshot.Fork()
				assert.NoError(t, err)

				tc.op(snapshot)

				// Modifications should be applied.
				compareStates(t, tc.modifiedState, getSnapshotState(t, snapshot))
			})
			t.Run(fmt.Sprintf("%s: %s fork & revert", name, tc.name), func(t *testing.T) {
				snapshot := startSnapshot(t, snapshotFactory, tc.state)

				err := snapshot.Fork()
				assert.NoError(t, err)

				tc.op(snapshot)

				err = snapshot.Revert()
				assert.NoError(t, err)

				// Modifications should no longer be applied.
				compareStates(t, tc.state, getSnapshotState(t, snapshot))
			})
			t.Run(fmt.Sprintf("%s: %s fork & commit", name, tc.name), func(t *testing.T) {
				snapshot := startSnapshot(t, snapshotFactory, tc.state)

				err := snapshot.Fork()
				assert.NoError(t, err)

				tc.op(snapshot)

				err = snapshot.Commit()
				assert.NoError(t, err)

				// Modifications should be applied.
				compareStates(t, tc.modifiedState, getSnapshotState(t, snapshot))
			})
		}
	}
}

func TestNode404(t *testing.T) {
	// Anything and everything that returns errNodeNotFound should be tested here.
	ops := []struct {
		name string
		op   func(ClusterSnapshot) error
	}{
		{"add pod", func(snapshot ClusterSnapshot) error {
			return snapshot.AddPod(BuildTestPod("p1", 0, 0), "node")
		}},
		{"remove pod", func(snapshot ClusterSnapshot) error {
			return snapshot.RemovePod("default", "p1", "node")
		}},
		{"get node", func(snapshot ClusterSnapshot) error {
			_, err := snapshot.NodeInfos().Get("node")
			return err
		}},
		{"remove node", func(snapshot ClusterSnapshot) error {
			return snapshot.RemoveNode("node")
		}},
	}

	for name, snapshotFactory := range snapshots {
		for _, op := range ops {
			t.Run(fmt.Sprintf("%s: %s empty", name, op.name),
				func(t *testing.T) {
					snapshot := snapshotFactory()

					// Empty snapshot - shouldn't be able to operate on nodes that are not here.
					err := op.op(snapshot)
					assert.Error(t, err)
				})

			t.Run(fmt.Sprintf("%s: %s fork", name, op.name),
				func(t *testing.T) {
					snapshot := snapshotFactory()

					node := BuildTestNode("node", 10, 100)
					err := snapshot.AddNode(node)
					assert.NoError(t, err)

					err = snapshot.Fork()
					assert.NoError(t, err)

					err = snapshot.RemoveNode("node")
					assert.NoError(t, err)

					// Node deleted after fork - shouldn't be able to operate on it.
					err = op.op(snapshot)
					assert.Error(t, err)

					err = snapshot.Commit()
					assert.NoError(t, err)

					// Node deleted before commit - shouldn't be able to operate on it.
					err = op.op(snapshot)
					assert.Error(t, err)
				})

			t.Run(fmt.Sprintf("%s: %s base", name, op.name),
				func(t *testing.T) {
					snapshot := snapshotFactory()

					node := BuildTestNode("node", 10, 100)
					err := snapshot.AddNode(node)
					assert.NoError(t, err)

					err = snapshot.RemoveNode("node")
					assert.NoError(t, err)

					// Node deleted from base - shouldn't be able to operate on it.
					err = op.op(snapshot)
					assert.Error(t, err)
				})
		}
	}
}

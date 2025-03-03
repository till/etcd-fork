// Copyright 2022 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/tests/v3/framework/config"
	"go.etcd.io/etcd/tests/v3/framework/e2e"
)

func TestAuthCluster(t *testing.T) {
	e2e.BeforeTest(t)
	cfg := &e2e.EtcdProcessClusterConfig{
		ClusterSize:   1,
		InitialToken:  "new",
		SnapshotCount: 2,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	epc, err := e2e.NewEtcdProcessCluster(ctx, t, cfg)
	if err != nil {
		t.Fatalf("could not start etcd process cluster (%v)", err)
	}
	defer func() {
		if err := epc.Close(); err != nil {
			t.Fatalf("could not close test cluster (%v)", err)
		}
	}()

	epcClient := epc.Client()
	createUsers(ctx, t, epcClient)

	if err := epcClient.AuthEnable(ctx); err != nil {
		t.Fatalf("could not enable Auth: (%v)", err)
	}

	testUserClientOpts := e2e.WithAuth("test", "testPassword")
	rootUserClientOpts := e2e.WithAuth("root", "rootPassword")

	// write more than SnapshotCount keys to single leader to make sure snapshot is created
	for i := 0; i <= 10; i++ {
		if err := epc.Client(testUserClientOpts).Put(ctx, fmt.Sprintf("/test/%d", i), "test", config.PutOptions{}); err != nil {
			t.Fatalf("failed to Put (%v)", err)
		}
	}

	// start second process
	if err := epc.StartNewProc(ctx, t, rootUserClientOpts); err != nil {
		t.Fatalf("could not start second etcd process (%v)", err)
	}

	// make sure writes to both endpoints are successful
	endpoints := epc.EndpointsV3()
	assert.Equal(t, len(endpoints), 2)
	for _, endpoint := range epc.EndpointsV3() {
		if err := epc.Client(testUserClientOpts, e2e.WithEndpoints([]string{endpoint})).Put(ctx, "/test/key", endpoint, config.PutOptions{}); err != nil {
			t.Fatalf("failed to write to Put to %q (%v)", endpoint, err)
		}
	}

	// verify all nodes have exact same revision and hash
	assert.Eventually(t, func() bool {
		hashKvs, err := epc.Client(rootUserClientOpts).HashKV(ctx, 0)
		if err != nil {
			t.Logf("failed to get HashKV: %v", err)
			return false
		}
		if len(hashKvs) != 2 {
			t.Logf("not exactly 2 hashkv responses returned: %d", len(hashKvs))
			return false
		}
		if hashKvs[0].Header.Revision != hashKvs[1].Header.Revision {
			t.Logf("The two members' revision (%d, %d) are not equal", hashKvs[0].Header.Revision, hashKvs[1].Header.Revision)
			return false
		}
		assert.Equal(t, hashKvs[0].Hash, hashKvs[1].Hash)
		return true
	}, time.Second*5, time.Millisecond*100)

}

func createUsers(ctx context.Context, t *testing.T, client *e2e.EtcdctlV3) {
	if _, err := client.UserAdd(ctx, "root", "rootPassword", config.UserAddOptions{}); err != nil {
		t.Fatalf("could not add root user (%v)", err)
	}
	if _, err := client.RoleAdd(ctx, "root"); err != nil {
		t.Fatalf("could not create 'root' role (%v)", err)
	}
	if _, err := client.UserGrantRole(ctx, "root", "root"); err != nil {
		t.Fatalf("could not grant root role to root user (%v)", err)
	}

	if _, err := client.RoleAdd(ctx, "test"); err != nil {
		t.Fatalf("could not create 'test' role (%v)", err)
	}
	if _, err := client.RoleGrantPermission(ctx, "test", "/test/", "/test0", clientv3.PermissionType(clientv3.PermReadWrite)); err != nil {
		t.Fatalf("could not RoleGrantPermission (%v)", err)
	}
	if _, err := client.UserAdd(ctx, "test", "testPassword", config.UserAddOptions{}); err != nil {
		t.Fatalf("could not add user test (%v)", err)
	}
	if _, err := client.UserGrantRole(ctx, "test", "test"); err != nil {
		t.Fatalf("could not grant test role user (%v)", err)
	}
}

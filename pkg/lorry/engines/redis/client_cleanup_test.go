/*
Copyright (C) 2022-2024 ApeCloud Co., Ltd

This file is part of KubeBlocks project

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package redis

import (
	"context"
	"reflect"
	"testing"

	goredis "github.com/redis/go-redis/v9"

	"github.com/apecloud/kubeblocks/pkg/lorry/engines/models"
)

type recordedRedisCommandExecutor struct {
	commands [][]interface{}
	results  []interface{}
	errors   []error
}

func (executor *recordedRedisCommandExecutor) Do(_ context.Context, args ...interface{}) *goredis.Cmd {
	executor.commands = append(executor.commands, args)
	cmd := goredis.NewCmd(context.Background(), args...)
	index := len(executor.commands) - 1
	if index < len(executor.errors) && executor.errors[index] != nil {
		cmd.SetErr(executor.errors[index])
	} else if index < len(executor.results) {
		cmd.SetVal(executor.results[index])
	}
	return cmd
}

func TestUpdateObservedRoleTriggersOnlyOnDemotion(t *testing.T) {
	manager := &Manager{}

	if triggered, _ := manager.updateObservedRole(models.SECONDARY); triggered {
		t.Fatal("initial secondary observation must not trigger client cleanup")
	}
	if triggered, _ := manager.updateObservedRole(models.PRIMARY); triggered {
		t.Fatal("promotion must not trigger client cleanup")
	}

	triggered, generation := manager.updateObservedRole(models.SECONDARY)
	if !triggered {
		t.Fatal("primary to secondary transition must trigger client cleanup")
	}
	if !manager.isCurrentDemotion(generation) {
		t.Fatal("the demotion generation should be current")
	}

	if triggered, _ := manager.updateObservedRole(models.SECONDARY); triggered {
		t.Fatal("repeated secondary observations must not trigger duplicate cleanup")
	}
	if triggered, _ := manager.updateObservedRole(models.PRIMARY); triggered {
		t.Fatal("promotion must not trigger client cleanup")
	}
	if manager.isCurrentDemotion(generation) {
		t.Fatal("promotion must cancel cleanup scheduled for the previous demotion")
	}
}

func TestDisconnectRedisClients(t *testing.T) {
	executor := &recordedRedisCommandExecutor{
		results: []interface{}{
			"id=11 addr=10.0.0.10:40000 flags=N name= user=default\n" +
				"id=12 addr=10.0.0.11:40001 flags=P name=events user=app\n" +
				"id=13 addr=127.0.0.1:40002 flags=N name= user=default\n" +
				"id=14 addr=10.0.0.12:40003 flags=N name=sentinel-test-cmd user=default\n" +
				"id=15 addr=10.0.0.13:40004 flags=N name= user=kbreplicator-sentinel\n" +
				"id=16 addr=10.0.0.14:40005 flags=S name= user=kbreplicator\n" +
				"id=17 addr=10.0.0.15:40006 flags=N name= user=customer\n",
			int64(1),
			int64(1),
			int64(0),
		},
	}

	disconnectedClients, err := disconnectRedisClients(context.Background(), executor)
	if err != nil {
		t.Fatalf("disconnectRedisClients returned an error: %v", err)
	}
	if disconnectedClients != 2 {
		t.Fatalf("unexpected disconnected client count: %d", disconnectedClients)
	}

	expected := [][]interface{}{
		{"CLIENT", "LIST"},
		{"CLIENT", "KILL", "ID", "11"},
		{"CLIENT", "KILL", "ID", "12"},
		{"CLIENT", "KILL", "ID", "17"},
	}
	if !reflect.DeepEqual(executor.commands, expected) {
		t.Fatalf("unexpected Redis commands: %#v", executor.commands)
	}
}

func TestExternalRedisClientIDs(t *testing.T) {
	clientList := "id=21 addr=[::1]:50000 flags=N name= user=default\n" +
		"id=22 addr=not-an-address flags=N name= user=default\n" +
		"id=23 addr=10.0.0.20:50001 flags=M name= user=(superuser)\n" +
		"id=24 addr=10.0.0.21:50002 flags=N name= user=customer"

	expected := []string{"24"}
	if actual := externalRedisClientIDs(clientList); !reflect.DeepEqual(actual, expected) {
		t.Fatalf("unexpected external client IDs: %#v", actual)
	}
}

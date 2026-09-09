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
	"errors"
	"net"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/apecloud/kubeblocks/pkg/lorry/engines/models"
)

const (
	// Keep sweeping while the primary Service endpoint converges after a failover.
	demotedClientCleanupMaxAttempts   = 120
	demotedClientCleanupMinAttempts   = 10
	demotedClientCleanupQuietAttempts = 3
	demotedClientCleanupInterval      = time.Second
	demotedClientCleanupTimeout       = 2 * time.Second
)

type redisCommandExecutor interface {
	Do(ctx context.Context, args ...interface{}) *goredis.Cmd
}

func (mgr *Manager) observeRole(role string) string {
	shouldDisconnect, generation := mgr.updateObservedRole(role)
	if shouldDisconnect {
		mgr.Logger.Info("Redis primary demoted, scheduling client disconnection")
		go mgr.disconnectClientsAfterDemotion(generation)
	}
	return role
}

func (mgr *Manager) updateObservedRole(role string) (bool, uint64) {
	if role != models.PRIMARY && role != models.SECONDARY {
		return false, 0
	}

	mgr.roleMu.Lock()
	defer mgr.roleMu.Unlock()

	if mgr.observedRole == role {
		return false, mgr.roleGeneration
	}

	previousRole := mgr.observedRole
	mgr.observedRole = role
	mgr.roleGeneration++
	return previousRole == models.PRIMARY && role == models.SECONDARY, mgr.roleGeneration
}

func (mgr *Manager) disconnectClientsAfterDemotion(generation uint64) {
	quietAttempts := 0
	for attempt := 1; attempt <= demotedClientCleanupMaxAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(demotedClientCleanupInterval)
		}

		if !mgr.isCurrentDemotion(generation) {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), demotedClientCleanupTimeout)
		disconnectedClients, err := disconnectRedisClients(ctx, mgr.client)
		cancel()
		if err != nil {
			mgr.Logger.Error(err, "failed to disconnect Redis clients after demotion", "attempt", attempt)
			quietAttempts = 0
			continue
		}

		if disconnectedClients > 0 {
			quietAttempts = 0
			mgr.Logger.Info("disconnected Redis clients after demotion",
				"attempt", attempt,
				"clients", disconnectedClients)
			continue
		}

		if attempt < demotedClientCleanupMinAttempts {
			continue
		}
		quietAttempts++
		if quietAttempts >= demotedClientCleanupQuietAttempts {
			mgr.Logger.Info("completed Redis client cleanup after demotion", "attempts", attempt)
			return
		}
	}
	mgr.Logger.Info("completed Redis client cleanup after demotion",
		"attempts", demotedClientCleanupMaxAttempts,
		"reason", "maximum cleanup attempts reached")
}

func (mgr *Manager) isCurrentDemotion(generation uint64) bool {
	mgr.roleMu.Lock()
	defer mgr.roleMu.Unlock()
	return mgr.observedRole == models.SECONDARY && mgr.roleGeneration == generation
}

func disconnectRedisClients(ctx context.Context, client redisCommandExecutor) (int, error) {
	clientList, err := client.Do(ctx, "CLIENT", "LIST").Text()
	if err != nil {
		return 0, err
	}

	clientIDs := externalRedisClientIDs(clientList)
	disconnectedClients := 0
	disconnectErrors := make([]error, 0)
	for _, clientID := range clientIDs {
		killedClients, err := client.Do(ctx, "CLIENT", "KILL", "ID", clientID).Int64()
		if err != nil {
			disconnectErrors = append(disconnectErrors, err)
			continue
		}
		disconnectedClients += int(killedClients)
	}
	return disconnectedClients, errors.Join(disconnectErrors...)
}

func externalRedisClientIDs(clientList string) []string {
	clientIDs := make([]string, 0)
	for _, line := range strings.Split(clientList, "\n") {
		fields := parseRedisClientLine(line)
		if isExternalRedisClient(fields) {
			clientIDs = append(clientIDs, fields["id"])
		}
	}
	return clientIDs
}

func parseRedisClientLine(line string) map[string]string {
	client := make(map[string]string)
	for _, field := range strings.Fields(line) {
		key, value, found := strings.Cut(field, "=")
		if found {
			client[key] = value
		}
	}
	return client
}

func isExternalRedisClient(client map[string]string) bool {
	// Preserve local probes, replication links, and Sentinel management connections.
	if client["id"] == "" || isLocalRedisClientAddress(client["addr"]) {
		return false
	}
	if strings.ContainsAny(client["flags"], "MS") {
		return false
	}
	if client["user"] == "kbreplicator" || client["user"] == "kbreplicator-sentinel" {
		return false
	}
	return !strings.HasPrefix(client["name"], "sentinel-")
}

func isLocalRedisClientAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return true
	}
	ip := net.ParseIP(host)
	return ip == nil || ip.IsLoopback()
}

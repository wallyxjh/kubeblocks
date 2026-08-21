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

package polardbpostgres

import (
	"context"

	"github.com/pkg/errors"

	"github.com/apecloud/kubeblocks/pkg/lorry/dcs"
	"github.com/apecloud/kubeblocks/pkg/lorry/engines"
	"github.com/apecloud/kubeblocks/pkg/lorry/engines/postgres/officalpostgres"
)

const fenceReason = "polardb-postgresql fencing before role change"

type Manager struct {
	officalpostgres.Manager
}

var _ engines.DBManager = &Manager{}

func NewManager(properties engines.Properties) (engines.DBManager, error) {
	base, err := officalpostgres.NewManager(properties)
	if err != nil {
		return nil, err
	}
	officialMgr, ok := base.(*officalpostgres.Manager)
	if !ok {
		return nil, errors.Errorf("unexpected official postgres manager type %T", base)
	}
	return &Manager{Manager: *officialMgr}, nil
}

func (mgr *Manager) Demote(ctx context.Context) error {
	isLeader, err := mgr.IsLeader(ctx, nil)
	if err == nil && !isLeader {
		mgr.Logger.Info("current member is already standby, skip polardb-postgresql fencing")
		return nil
	}
	if err != nil {
		mgr.Logger.Info("role probe failed before fencing, continue with stop fencing", "error", err.Error())
	} else if err = mgr.Lock(ctx, fenceReason); err != nil {
		mgr.Logger.Info("readonly fencing failed, continue with stop fencing", "error", err.Error())
	}

	return mgr.Manager.Demote(ctx)
}

func (mgr *Manager) Recover(ctx context.Context, cluster *dcs.Cluster) error {
	mgr.Logger.Info("recover polardb-postgresql member by re-following leader")
	return mgr.rejoin(ctx, cluster)
}

func (mgr *Manager) JoinCurrentMemberToCluster(ctx context.Context, cluster *dcs.Cluster) error {
	mgr.Logger.Info("join polardb-postgresql member by following current leader")
	return mgr.rejoin(ctx, cluster)
}

func (mgr *Manager) rejoin(ctx context.Context, cluster *dcs.Cluster) error {
	if cluster == nil {
		return errors.New("cluster is nil")
	}
	if cluster.Leader == nil {
		mgr.Logger.Info("cluster has no leader, skip rejoin until leader is elected")
		return nil
	}
	if cluster.Leader.Name == mgr.CurrentMemberName {
		mgr.Logger.Info("current member is leader, skip rejoin")
		return nil
	}
	return mgr.Follow(ctx, cluster)
}

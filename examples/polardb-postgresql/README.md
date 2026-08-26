# PolarDB PostgreSQL

This directory contains KubeBlocks 0.9 examples for running PolarDB PostgreSQL
with the KB-native production HA workflow.

The KubeBlocks Helm chart in this branch publishes an optional
`polardb-postgresql` Addon CR. It is intentionally not auto-installed. To enable
it through the Addon controller, the engine chart
`polardb-postgresql-0.9.3.tgz` must be available from `addonChartLocationBase` or
from the configured `addonChartsImage`.

The current KB 0.9 integration expects a Patroni-based ComponentDefinition named
`polardb-postgresql-ha`. The controller-side adapter in this branch disables
lorry's built-in HA loop for the `polardb-postgresql` builtin handler, so Patroni
remains the only database HA controller while KubeBlocks drives lifecycle Ops.

## Create a Cluster

```bash
kubectl create namespace kb-polardb-pg
kubectl apply -f examples/polardb-postgresql/cluster.yaml
kubectl get cluster,pod -n kb-polardb-pg
```

The example creates a two-replica component named `postgresql` from
`polardb-postgresql-ha`.

## Enable HA Backup Policy

```bash
kubectl apply -f examples/polardb-postgresql/ha/backuppolicytemplate.yaml
```

The template reuses the PostgreSQL `postgres-basebackup` ActionSet and makes
`pg-basebackup` available to PolarDB PostgreSQL clusters.

## Run HA Drill

```bash
NAMESPACE=kb-polardb-pg CLUSTER=polardb-pg COMPONENT=postgresql \
  bash examples/polardb-postgresql/ha/scripts/kb09-polardb-pg-ha-drill.sh
```

Run the full production HA closure, including rebuild and backup/restore drill:

```bash
WITH_REBUILD=true WITH_BACKUP=true APPLY_BACKUP_POLICY_TEMPLATE=true \
  bash examples/polardb-postgresql/ha/scripts/kb09-polardb-pg-ha-drill.sh
```

For KB 0.9 test environments, verify that dataprotection uses pullable tool
images. In the verified test cluster, `DATASAFED_IMAGE` was set to
`apecloud-registry.cn-zhangjiakou.cr.aliyuncs.com/apecloud/datasafed:0.2.0`.

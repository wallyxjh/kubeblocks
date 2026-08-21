# PolarDB PostgreSQL HA Adapter for KubeBlocks 0.9

This directory contains the KubeBlocks 0.9 integration samples for the `polardb-postgresql` lorry adapter.

The adapter is intentionally thin:

- it registers `polardb-postgresql` as a built-in lorry engine;
- it reuses the existing official PostgreSQL replication, LSN, timeline, switchover and `pg_rewind` logic;
- it strengthens demotion by applying read-only fencing before stop fencing;
- it maps member rejoin and recover to the existing PostgreSQL follow/rewind path.

## ComponentDefinition hook

Use `polardb-postgresql` as the built-in handler in the PolarDB-PG ComponentDefinition lifecycle actions:

```yaml
lifecycleActions:
  roleProbe:
    builtinHandler: polardb-postgresql
  switchover:
    withCandidate:
      builtinHandler: polardb-postgresql
    withoutCandidate:
      builtinHandler: polardb-postgresql
  memberJoin:
    builtinHandler: polardb-postgresql
  memberLeave:
    builtinHandler: polardb-postgresql
  readonly:
    builtinHandler: polardb-postgresql
  readwrite:
    builtinHandler: polardb-postgresql
```

The lorry sidecar also needs the `polardb-postgresql` component binding included in the lorry image at `config/lorry/components/binding_polardb_postgresql.yaml`.

## Operations

```bash
kubectl apply -f examples/polardb-postgresql/switchover.yaml
kubectl apply -f examples/polardb-postgresql/rebuild-instance.yaml
```

Replace the cluster, component and instance names before applying to a real cluster.

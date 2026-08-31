---
title: KubeBlocks 0.9 PolarDB PostgreSQL HA Deployment and Test Guide
description: Release deployment and acceptance test procedures for PolarDB PostgreSQL HA on KubeBlocks 0.9.
keywords: [kubeblocks, polardb postgresql, ha, deployment, test, kbcli]
---

# KubeBlocks 0.9 PolarDB PostgreSQL HA 部署与测试指南

本指南对应 [生产可用 HA 技术方案](polardb-postgresql-ha-technical-solution-zh.md) 和已验证发布 [v0.9.3-polardb-ha.2](https://github.com/wallyxjh/kubeblocks/releases/tag/v0.9.3-polardb-ha.2)。

## 1. 选择部署方式

| 目标 | 推荐方式 | 说明 |
| --- | --- | --- |
| 正式环境安装或升级 KubeBlocks 控制面 | Helm + release values | 唯一能同时固定 manager、tools、datascript、dataprotection 和 addon charts digest 的方式。 |
| 从发布的 addon index 安装和启用插件 | `kbcli addon` | 适用于 index 已指向批准 addon charts digest 的环境。 |
| 创建 PolarDB PostgreSQL Cluster | YAML + `kubectl apply` | 本方案直接引用不可变 `ComponentDefinition`，这是当前 KB 0.9 的权威创建方式。 |
| 日常查询、switchover、rebuild、backup、restore | `kbcli cluster` 或 YAML `OpsRequest` | `kbcli` 简化日常操作；YAML 适合 GitOps、审计与 CI。 |

:::important

KB 0.9 的 `kbcli cluster create --cluster-definition ...` 使用旧 ClusterDefinition 模型，不能完整表达本方案的直接 `ComponentDefinition` Cluster。因此不要用它创建 `polardb-postgresql-ha-vN` 集群；使用第 4 节的 YAML。

:::

## 2. 部署前检查

### 2.1 必需条件

- `kubectl`、`helm` 和 `kbcli` 均指向同一目标 Kubernetes 集群。
- KubeBlocks 为 0.9.x，`kbcli` 建议使用 0.9.3 或与控制面兼容的版本。
- 已配置可用 StorageClass；生产环境使用远程或复制存储，不使用 node-local LVM、hostpath 或节点本地 BackupRepo。
- 生产 HA 至少三个可调度节点并跨故障域；单节点仅可用于功能测试。
- 私有 GHCR 包已配置 image pull secret。

```bash
kubectl version --short
helm version --short
kbcli version
kubectl get nodes -L topology.kubernetes.io/zone
kubectl get storageclass
kubectl get backuprepo -A
```

### 2.2 获取已批准 release digest

从 release 变更单或 [Release v0.9.3-polardb-ha.2](https://github.com/wallyxjh/kubeblocks/releases/tag/v0.9.3-polardb-ha.2) 取得所有 digest。示例中的 `REPLACE_*` 不能直接用于部署。

本次 KB 0.9 回归验证所用 manager 为：

```text
ghcr.io/wallyxjh/kubeblocks@sha256:71afde7363bb9868f4ff22330198972789020d033ba34646a691389a558f00e9
```

生产变更单还必须固定 tools、datascript、dataprotection、addon charts、Spilo、PgBouncer、PostgreSQL exporter 和 datasafed 的 digest。

### 2.3 私有镜像仓库访问

仅在 GHCR 包为私有时执行。令牌仅授予 `read:packages` 所需权限。

```bash
kubectl create namespace kb-system --dry-run=client -o yaml | kubectl apply -f -

kubectl -n kb-system create secret docker-registry ghcr-pull \
  --docker-server=ghcr.io \
  --docker-username='<github-user>' \
  --docker-password='<read-packages-token>'
```

## 3. 部署 KubeBlocks 与 PolarDB PostgreSQL addon

### 3.1 正式环境：Helm + digest 固定

获取源码或对应 release chart，并复制两份 release values。禁止在生产环境以可变 tag 覆盖这些文件。

```bash
git clone --branch v0.9.3-polardb-ha.2 https://github.com/wallyxjh/kubeblocks.git
cd kubeblocks

cp examples/polardb-postgresql/production/values-release.example.yaml \
  /secure-change-record/kubeblocks-release-values.yaml
cp examples/polardb-postgresql/production/addon-values-production.example.yaml \
  /secure-change-record/polardb-postgresql-addon-values.yaml
```

在两个副本中替换全部 `REPLACE_*` 值，并在变更单中保留 Helm render 结果。安装控制面：

```bash
helm upgrade --install kubeblocks deploy/helm \
  --namespace kb-system \
  --create-namespace \
  --values /secure-change-record/kubeblocks-release-values.yaml \
  --set image.imagePullSecrets[0].name=ghcr-pull \
  --wait --timeout 10m

kubectl rollout status deployment/kubeblocks -n kb-system --timeout=10m
kubectl get deployment kubeblocks -n kb-system \
  -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
```

首次安装使用 `polardb-postgresql-ha-v1`。如果环境中已存在由其他 Helm release 管理的 v1 资源，或需要调整不可变 ComponentDefinition，创建新版本定义和匹配 Helm release，例如 v2：

```bash
export ADDON_RELEASE=kb-addon-polardb-postgresql-v2
export COMPONENT_DEFINITION=polardb-postgresql-ha-v2

helm upgrade --install "$ADDON_RELEASE" deploy/addons/polardb-postgresql \
  --namespace kb-system \
  --values /secure-change-record/polardb-postgresql-addon-values.yaml \
  --set ha.componentDefinition.name="$COMPONENT_DEFINITION" \
  --wait --timeout 10m

kubectl get componentdefinition "$COMPONENT_DEFINITION"
```

不要通过修改 Helm ownership annotation、`--force` 或原地修改 ComponentDefinition 来解决冲突。旧集群继续使用旧定义；跨定义迁移通过计划迁移或备份恢复完成。

### 3.2 `kbcli`：安装与启用已发布 addon

仅在 addon index 返回的 chart 与批准的 release 工件一致时使用此路径。先检查索引和可用版本：

```bash
kbcli addon index list
kbcli addon search polardb-postgresql
kbcli addon list | grep polardb-postgresql
```

安装并启用：

```bash
kbcli addon install polardb-postgresql --version 0.9.3

kbcli addon enable polardb-postgresql \
  --set image.digest=sha256:REPLACE_APPROVED_SPILO_DIGEST \
  --set metrics.image.digest=sha256:REPLACE_APPROVED_POSTGRES_EXPORTER_DIGEST \
  --set pgbouncer.image.digest=sha256:REPLACE_APPROVED_PGBOUNCER_DIGEST \
  --set ha.componentDefinition.name=polardb-postgresql-ha-v1

kbcli addon describe polardb-postgresql
kubectl get addon polardb-postgresql -o jsonpath='{.status.phase}{"\n"}'
kubectl get componentdefinition polardb-postgresql-ha-v1
```

`kbcli kubeblocks install --version <approved-0.9-version>` 可用于安装标准 KubeBlocks 0.9，但不能一次表达本方案所有第一方和运行时镜像 digest。正式 release 仍使用第 3.1 节 Helm values。

若 `kbcli addon describe` 显示 `Failed`，并出现已有 ConfigMap 或 ComponentDefinition 的 Helm ownership 冲突，不要强制重试；按第 3.1 节发布一个新版本化 ComponentDefinition 和 Helm release。

## 4. YAML 创建集群

以下清单创建两副本 Patroni 集群。将资源、StorageClass、故障域拓扑和 ComponentDefinition 名称替换为目标环境的批准值。

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: polardb-prod
---
apiVersion: apps.kubeblocks.io/v1alpha1
kind: Cluster
metadata:
  name: polardb-pg
  namespace: polardb-prod
spec:
  terminationPolicy: Delete
  componentSpecs:
    - name: postgresql
      componentDef: polardb-postgresql-ha-v1
      replicas: 2
      disableExporter: false
      affinity:
        podAntiAffinity: Required
        topologyKeys:
          - kubernetes.io/hostname
          - topology.kubernetes.io/zone
        tenancy: SharedNode
      resources:
        requests:
          cpu: "1"
          memory: 2Gi
        limits:
          cpu: "2"
          memory: 4Gi
      volumeClaimTemplates:
        - name: data
          spec:
            accessModes:
              - ReadWriteOnce
            storageClassName: REPLACE_REMOTE_STORAGECLASS
            resources:
              requests:
                storage: 100Gi
```

保存为 `polardb-pg.yaml` 后，先进行服务端校验再创建：

```bash
kubectl create namespace polardb-prod --dry-run=client -o yaml | kubectl apply -f -
kubectl apply --dry-run=server -f polardb-pg.yaml
kubectl apply -f polardb-pg.yaml

kubectl wait --for=condition=Ready cluster/polardb-pg \
  -n polardb-prod --timeout=15m
kubectl get cluster,instanceset,pod -n polardb-prod -o wide
```

仓库提供的最小示例为 `examples/polardb-postgresql/cluster.yaml`。该文件适用于功能测试；正式环境使用本节清单并设置 remote StorageClass、资源和 `Required` 反亲和。

## 5. 创建后基线验证

### 5.1 使用 `kubectl` 和 `kbcli` 查看状态

```bash
kubectl get cluster polardb-pg -n polardb-prod -o wide
kubectl get instanceset -n polardb-prod -o wide
kubectl get pod -n polardb-prod -o wide

kbcli cluster describe polardb-pg -n polardb-prod
kbcli cluster list-instances polardb-pg -n polardb-prod
```

预期结果：Cluster 为 `Running`，`Ready=True`，InstanceSet Ready/Available 等于副本数，两个 Pod 均就绪。

### 5.2 验证主备角色和复制

以下命令不会暴露数据库密码，使用 Pod 内本地连接。输出应恰好包含一个 `f|off` 主库和至少一个 `t|on` 备库。

```bash
for pod in $(kubectl get pod -n polardb-prod \
  -l app.kubernetes.io/instance=polardb-pg,apps.kubeblocks.io/component-name=postgresql \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}'); do
  printf '%s: ' "$pod"
  kubectl exec -n polardb-prod "$pod" -c postgresql -- \
    psql -U postgres -d postgres -Atc \
    "SELECT pg_is_in_recovery(), current_setting('transaction_read_only')"
done
```

验证主写从读。将 `<primary-pod>` 与 `<standby-pod>` 替换为上一步结果：

```bash
kubectl exec -n polardb-prod <primary-pod> -c postgresql -- \
  psql -U postgres -d postgres -v ON_ERROR_STOP=1 -Atc \
  'CREATE TABLE IF NOT EXISTS ha_smoke (id integer primary key); INSERT INTO ha_smoke (id) VALUES (1) ON CONFLICT (id) DO NOTHING; SELECT count(*) FROM ha_smoke WHERE id = 1'

kubectl exec -n polardb-prod <standby-pod> -c postgresql -- \
  psql -U postgres -d postgres -v ON_ERROR_STOP=1 -Atc \
  'SELECT count(*) FROM ha_smoke WHERE id = 1'
```

两次查询均应返回 `1`。

### 5.3 验证固定 digest

```bash
kubectl get pod -n polardb-prod -o json | jq -r '
  .items[] |
  [.metadata.name,
   ([((.spec.initContainers // [])[] | .image), (.spec.containers[] | .image)] | all(contains("@sha256:")) | tostring),
   ([((.status.initContainerStatuses // [])[] | .imageID), (.status.containerStatuses[] | .imageID)] | all(contains("@sha256:")) | tostring)] |
  @tsv'
```

每个 Pod 的两列均应为 `true`。这是对 [PR #19](https://github.com/wallyxjh/kubeblocks/pull/19) 修复路径的运行时验证。

## 6. HA 操作测试

所有测试先在隔离集群执行。生产切换必须有变更单、健康备库和业务连接排空计划；不要用删除 Pod 替代节点失联、网络分区或存储分裂演练。

### 6.1 计划内 switchover

自动选择零延迟健康候选：

```bash
kbcli cluster promote polardb-pg \
  --component postgresql \
  --auto-approve \
  -n polardb-prod
```

指定候选：

```bash
kbcli cluster promote polardb-pg \
  --component postgresql \
  --instance polardb-pg-postgresql-0 \
  --auto-approve \
  -n polardb-prod
```

等价 YAML 见 `examples/polardb-postgresql/ha/ops-switchover-auto.yaml` 和 `ops-switchover-candidate.yaml`。复制清单后必须修改 `metadata.namespace`、`spec.clusterName` 和实例名：

```bash
kubectl apply -f ops-switchover-auto.yaml
kubectl get opsrequest -n polardb-prod -w
```

完成后重跑第 5.2 节，确认主备身份互换且仍只有一个可写主库。

### 6.2 rejoin、rebuild 与逻辑 fencing

- logical fencing 使用 `HorizontalScaling` 将已确认降级的旧主离线；示例见 `ops-fence-instance.yaml`。
- rejoin 使用 `HorizontalScaling` 将离线副本重新加入；示例见 `ops-rejoin-instance.yaml`。
- 出现时间线分叉、数据损坏或存储历史不可信时使用 `RebuildInstance`；示例见 `ops-rebuild-instance.yaml`。

`kbcli` 可直接发起副本重建：

```bash
kbcli cluster rebuild-instance polardb-pg \
  --instances polardb-pg-postgresql-0 \
  --auto-approve \
  -n polardb-prod
```

执行 fencing 或 rejoin 前必须确认旧主已降级。对节点失联、网络分区、存储分裂必须先完成物理 fencing；具体步骤见 [生产 HA Runbook](../examples/polardb-postgresql/production/RUNBOOK.md)。

### 6.3 备份与恢复演练

先确认 BackupRepo、BackupPolicy 和数据保护控制器：

```bash
kubectl get backuprepo -A
kbcli cluster describe-backup-policy polardb-pg -n polardb-prod
```

创建保留七天的 `pg-basebackup`：

```bash
kbcli cluster backup polardb-pg \
  --method pg-basebackup \
  --name polardb-pg-ha-drill-backup \
  --deletion-policy Retain \
  --retention-period 168h \
  -n polardb-prod

kbcli cluster list-backups polardb-pg -n polardb-prod
```

从完成的备份恢复临时集群：

```bash
kbcli cluster restore polardb-pg-restore-drill \
  --backup polardb-pg-ha-drill-backup \
  --backup-namespace polardb-prod \
  -n polardb-prod

kubectl wait --for=condition=Ready cluster/polardb-pg-restore-drill \
  -n polardb-prod --timeout=20m
```

YAML 方式使用 `ops-backup.yaml` 与 `ops-restore-drill.yaml`，同样需要替换所有名称和命名空间。恢复验证完成后，只删除演练 Cluster，不删除源 Cluster 或备份。

## 7. 一键功能回归

在可销毁测试集群、BackupRepo 就绪且数据保护镜像可拉取的前提下，运行完整 HA drill：

```bash
NAMESPACE=kb-polardb-pg-e2e \
CLUSTER=polardb-pg-e2e \
COMPONENT=postgresql \
WITH_REBUILD=true \
WITH_BACKUP=true \
APPLY_BACKUP_POLICY_TEMPLATE=true \
make test-polardb-postgresql-ha
```

该脚本安装 addon、创建两副本 Cluster、等待 `Running`，再验证 switchover、逻辑 fencing、rejoin、rebuild、backup 和 restore。使用 `CLEANUP=true` 仅在确认测试资源可删除时清理。

```bash
NAMESPACE=kb-polardb-pg-e2e \
CLUSTER=polardb-pg-e2e \
CLEANUP=true \
WITH_REBUILD=true \
WITH_BACKUP=true \
make test-polardb-postgresql-ha
```

## 8. 生产验收清单

在声明目标环境生产 HA 达标前，逐项记录证据：

- [ ] 所有 KubeBlocks、addon 和数据库运行时镜像均按 digest 固定。
- [ ] 生产副本数、节点数、拓扑与远程/复制存储符合 RPO/RTO 目标。
- [ ] BackupRepo 位于数据库故障域之外，已成功备份并恢复。
- [ ] 无主库、复制延迟、备份失败和恢复演练失败告警均已送达值班渠道。
- [ ] 节点失联、网络分区和存储分裂均完成物理 fencing、故障注入、rejoin/rebuild 与 RPO/RTO 记录。
- [ ] 所有 OpsRequest、Patroni leader history、fencing provider 和恢复演练证据已附到变更或演练记录。

上述条件未全部满足时，本方案应标记为“已实现 HA 能力，未完成目标环境生产 HA 验收”。

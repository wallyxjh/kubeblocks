---
title: KubeBlocks 0.9 PolarDB Mongo 兼容版部署与测试指南
description: 使用 YAML 和 kbcli 部署、备份、恢复和验证 PolarDB Mongo 兼容版。
keywords: kubeblocks kbcli yaml polardb mongo ferretdb backup restore
---

# KubeBlocks 0.9 PolarDB Mongo 兼容版部署与测试指南

## 前置条件

- KubeBlocks 和 kbcli 均为 0.9.x。
- 默认 StorageClass 可提供 ReadWriteOnce PVC。
- 至少有一个状态为 Ready 的 BackupRepo。
- 节点可拉取 FerretDB、postgres-documentdb 和 docker.io/mongo:7.0 镜像。

~~~bash
kubectl get backuprepo
kbcli version
~~~

## YAML 部署

install.yaml 包含两个 ComponentDefinition、ClusterDefinition、ActionSet、BackupPolicyTemplate、样例 Cluster 和 PDB。

~~~bash
kubectl apply -f examples/polardb-mongo-compat/install.yaml
kubectl wait --for=condition=Ready cluster/polardb-mongo-compat-sample --timeout=30m
kubectl get component,pod,pvc,pdb -l app.kubernetes.io/instance=polardb-mongo-compat-sample
~~~

期望状态：

~~~text
Cluster: Running
documentdb: 1/1 Running
ferretdb: 2/2 Running
PVC: Bound
~~~

## MongoDB 协议 smoke

~~~bash
kubectl apply -f examples/polardb-mongo-compat/smoke-mongosh-job.yaml
kubectl wait --for=condition=complete job/polardb-mongo-compat-smoke --timeout=5m
kubectl logs job/polardb-mongo-compat-smoke
~~~

该 Job 验证 ping、insertMany、find、updateOne、deleteOne、索引和简单聚合。

## YAML 原生备份

安装完成后，KubeBlocks 应自动创建默认 BackupPolicy：

~~~bash
kubectl get backuppolicy polardb-mongo-compat-sample-documentdb-backup-policy
~~~

创建标准 Backup CR：

~~~bash
kubectl apply -f examples/polardb-mongo-compat/backup.yaml
kubectl wait --for=jsonpath='{.status.phase}'=Completed +  backup/polardb-mongo-compat-sample-backup --timeout=30m
kubectl get backup polardb-mongo-compat-sample-backup -o wide
~~~

期望 Backup 显示：

~~~text
METHOD: mongodump
REPO:   <ready BackupRepo>
STATUS: Completed
~~~

## YAML 原生恢复

恢复必须创建新 Cluster：

~~~bash
kubectl apply -f examples/polardb-mongo-compat/restore.yaml
kubectl wait --for=condition=Ready cluster/polardb-mongo-compat-restored --timeout=30m
kubectl get restore -n default
~~~

目标 Restore 的状态必须为 Completed。Cluster=Running 只代表运行组件已就绪；必须另外检查 Restore CR。

验证恢复后的数据时，使用新 Cluster 的 Secret 和 FerretDB Service：

~~~bash
CLUSTER=polardb-mongo-compat-restored
USER=$(kubectl get secret "$CLUSTER-documentdb-account-ferretdb" -o jsonpath='{.data.username}' | base64 -d)
PASS=$(kubectl get secret "$CLUSTER-documentdb-account-ferretdb" -o jsonpath='{.data.password}' | base64 -d)

kubectl run mongo-verify --rm -it --restart=Never --image=docker.io/mongo:7.0 -- +  mongosh "mongodb://$USER:$PASS@$CLUSTER-ferretdb:27017/kbcompat?authMechanism=SCRAM-SHA-256&authSource=postgres"
~~~

在 mongosh 中至少校验集合文档数、getIndexes() 和关键聚合结果。

## kbcli 原生备份与恢复

kbcli 使用相同的自动生成 BackupPolicy 和标准 CR：

~~~bash
kbcli cluster list-backup-policy polardb-mongo-compat-sample -n default

kbcli cluster backup polardb-mongo-compat-sample +  --name polardb-mongo-compat-kbcli-backup +  --method mongodump +  --policy polardb-mongo-compat-sample-documentdb-backup-policy +  --deletion-policy Retain +  -n default

kbcli cluster list-backups --name=polardb-mongo-compat-kbcli-backup -n default

kbcli cluster restore polardb-mongo-compat-cli +  --backup polardb-mongo-compat-kbcli-backup +  -n default

kubectl wait --for=condition=Ready cluster/polardb-mongo-compat-cli --timeout=30m
kubectl get restore -n default
~~~

## 端到端验收

建议在备份前写入带索引的独立测试集合，然后在恢复 Cluster 中比对：

| 校验项 | 期望 |
| --- | --- |
| 文档数 | 与源集合一致 |
| _id_ 和业务索引 | 均存在 |
| $group / $sum 聚合 | 与源集合一致 |
| Backup CR | Completed |
| Restore CR | Completed |
| 新 Cluster | Running |

本方案在 KubeBlocks 0.9.3 测试集群已完成两条路径验证：

1. YAML 创建 Backup 和 annotation 驱动的新 Cluster Restore。
2. kbcli cluster backup 和 kbcli cluster restore。

两条路径均验证了 25 条文档、复合唯一索引 bucket_1_sequence_1 和五组聚合结果恢复一致。

## 常见问题

### 没有生成 BackupPolicy

检查 ActionSet 和 BackupPolicyTemplate 是否已安装，并确认 Cluster 已完成一次 reconcile：

~~~bash
kubectl get actionset polardb-mongo-compat-mongodump
kubectl get backuppolicytemplate polardb-mongo-compat-backup-policy-template
kubectl get backuppolicy -n default
~~~

没有 BackupPolicy 时，手工运行 mongodump 只会产生普通 Job，不会产生 KubeBlocks Backup CR。

### Restore 已创建但尚未完成

~~~bash
kubectl get restore -n default -o wide
kubectl describe restore <restore-name>
kubectl get job -n default
~~~

确认目标 Cluster 的 DocumentDB 和 FerretDB 都已 Ready；Restore Job 通过目标 Cluster 的系统账号连接 FerretDB，而不是使用源 Cluster 的 Secret。

### 需要更高数据保护等级

本 ActionSet 是全量逻辑备份。事务一致性、增量、PITR、oplog 和后端物理恢复应由最终 HA PostgreSQL-compatible 平台及其备份策略承担。

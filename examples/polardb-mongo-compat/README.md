# PolarDB Mongo 兼容版（版本 B）

这个示例演示的是版本 B：`FerretDB` 作为 MongoDB 协议前端，后端用 PostgreSQL + DocumentDB extension 承载数据。

生产环境里，后端应替换成可运行 DocumentDB extension 的 HA PostgreSQL-compatible 底座。若选择 PolarDB-PG，需要先验证 DocumentDB extension 与目标内核版本兼容。

## 创建

```bash
kubectl apply -f examples/polardb-mongo-compat/install.yaml

kubectl wait --for=condition=Ready cluster/polardb-mongo-compat-sample --timeout=30m
```

如果你想换成别的 cluster 名字，需要同步修改 `install.yaml` 中的 Cluster、PDB、账号 Secret 名和 `kubectl wait` 对象名。前端到后端的地址通过 `serviceVarRef` 注入，不需要手工改后端 Service 名。

## 连接

```bash
USER=$(kubectl get secret polardb-mongo-compat-sample-documentdb-account-ferretdb -o jsonpath='{.data.username}' | base64 -d)
PASS=$(kubectl get secret polardb-mongo-compat-sample-documentdb-account-ferretdb -o jsonpath='{.data.password}' | base64 -d)
HOST=polardb-mongo-compat-sample-ferretdb
PORT=27017
DB=kbcompat

mongosh "mongodb://${USER}:${PASS}@${HOST}:${PORT}/${DB}?authMechanism=SCRAM-SHA-256&authSource=postgres"
```

## 验证

运行内置 smoke Job：

```bash
kubectl apply -f examples/polardb-mongo-compat/smoke-mongosh-job.yaml
kubectl wait --for=condition=complete job/polardb-mongo-compat-smoke --timeout=5m
kubectl logs job/polardb-mongo-compat-smoke
```

## KubeBlocks 原生备份与恢复

`install.yaml` 会注册 `mongodump` ActionSet 和 BackupPolicyTemplate。Cluster 创建后，KubeBlocks 自动生成 `polardb-mongo-compat-sample-documentdb-backup-policy`，因此备份会产生标准 `Backup` CR 并写入默认 `BackupRepo`。

### kbcli

```bash
kbcli cluster list-backup-policy polardb-mongo-compat-sample -n default
kbcli cluster backup polardb-mongo-compat-sample \
  --name polardb-mongo-compat-kbcli-backup \
  --method mongodump \
  --policy polardb-mongo-compat-sample-documentdb-backup-policy \
  --deletion-policy Retain \
  -n default

kbcli cluster list-backups --name=polardb-mongo-compat-kbcli-backup -n default
kbcli cluster restore polardb-mongo-compat-cli \
  --backup polardb-mongo-compat-kbcli-backup \
  -n default

kubectl wait --for=condition=Ready cluster/polardb-mongo-compat-cli --timeout=30m
kubectl get restore -n default
```

### YAML

```bash
kubectl get backuppolicy polardb-mongo-compat-sample-documentdb-backup-policy
kubectl apply -f examples/polardb-mongo-compat/backup.yaml
kubectl wait --for=jsonpath='{.status.phase}'=Completed backup/polardb-mongo-compat-sample-backup --timeout=30m
kubectl get backup polardb-mongo-compat-sample-backup
```

恢复使用新 Cluster，而不是向原 Cluster 覆盖导入。它会在 DocumentDB 和 FerretDB 都已就绪后自动创建标准 `Restore` CR 与 post-ready Job：

```bash
kubectl apply -f examples/polardb-mongo-compat/restore.yaml
kubectl wait --for=condition=Ready cluster/polardb-mongo-compat-restored --timeout=30m
kubectl get restore -n default
```

这个逻辑备份使用 `mongodump --archive --gzip`，恢复使用 `mongorestore --drop`。恢复 Job 使用目标 Cluster 自己的 `ferretdb` 系统账号，不依赖源 Cluster 的 Secret。它覆盖 dump 中的 namespace，因此 `restore.yaml` 只应用于新建且可清空的目标 Cluster。该流程是 KubeBlocks 原生 `Backup` / `Restore` 生命周期，但数据一致性仍是 MongoDB 逻辑导出的边界：不提供事务一致快照、增量备份、PITR、oplog 或 `rs.*` 语义。

也可以手工验证：

```javascript
db.version()
db.demo.insertOne({ name: "polardb-mongo-compat", mode: "ferretdb" })
db.demo.find().pretty()
db.demo.updateOne({ name: "polardb-mongo-compat" }, { $set: { checked: true } })
db.demo.countDocuments()
```

匿名连接只能完成 `ping` / `hello` 这类握手命令，业务读写必须带账号，并使用 `SCRAM-SHA-256`。

## 说明

- 前端端口是 `27017`，对外按 MongoDB URI 暴露。
- 后端端口是 `5432`，只在集群内部被 FerretDB 访问。
- 这个示例只负责把版本 B 的编排和连接关系固定下来，不承诺原生 MongoDB 全语义。
- 当前示例后端是单副本 DocumentDB-compatible PostgreSQL，用于验证编排、连接和兼容边界；生产环境需要替换成 HA PostgreSQL-compatible 后端。
- 已验证兼容项包括基础 CRUD、索引、简单聚合、`distinct`、`mongosh`、PyMongo、Go driver 和 `mongodump` / `mongorestore`。
- 不兼容或不能按原生 MongoDB 承诺的项包括 `rs.*`、事务提交、`createRole` / `grantRolesToUser` / `getRoles` 和完整分片语义。
- 技术方案见 `docs/polardb-mongo-ferretdb-technical-solution-zh.md`。
- 部署与测试指南见 `docs/polardb-mongo-ferretdb-deployment-test-guide-zh.md`。

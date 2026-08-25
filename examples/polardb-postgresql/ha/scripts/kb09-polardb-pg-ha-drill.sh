#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-kb-polardb-pg}"
CLUSTER="${CLUSTER:-polardb-pg}"
COMPONENT="${COMPONENT:-postgresql}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-900}"
WITH_REBUILD="${WITH_REBUILD:-false}"
WITH_BACKUP="${WITH_BACKUP:-false}"
DRILL_BACKUP_DELETION_POLICY="${DRILL_BACKUP_DELETION_POLICY:-Delete}"
DRILL_CLEANUP_RESTORE_CLUSTER="${DRILL_CLEANUP_RESTORE_CLUSTER:-true}"
DRILL_CLEANUP_BACKUP="${DRILL_CLEANUP_BACKUP:-true}"

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"
}

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

wait_for() {
  local desc="$1"
  local cmd="$2"
  local start
  start="$(date +%s)"
  while true; do
    if eval "$cmd"; then
      return 0
    fi
    if [ "$(( $(date +%s) - start ))" -ge "$TIMEOUT_SECONDS" ]; then
      die "timeout waiting for ${desc}"
    fi
    sleep 5
  done
}

wait_cluster_running() {
  wait_for "cluster ${CLUSTER} Running" \
    "test \"\$(kubectl get cluster -n '${NAMESPACE}' '${CLUSTER}' -o jsonpath='{.status.phase}' 2>/dev/null)\" = Running"
}

wait_ops() {
  local ops="$1"
  wait_for "opsrequest ${ops} Succeed" \
    "test \"\$(kubectl get opsrequest -n '${NAMESPACE}' '${ops}' -o jsonpath='{.status.phase}' 2>/dev/null)\" = Succeed"
}

role_pod() {
  local role="$1"
  kubectl get pod -n "${NAMESPACE}" \
    -l "app.kubernetes.io/instance=${CLUSTER},apps.kubeblocks.io/component-name=${COMPONENT},kubeblocks.io/role=${role}" \
    -o jsonpath='{.items[0].metadata.name}'
}

apply_switchover_auto() {
  local ops="ha-drill-switchover-$(date +%s)"
  kubectl apply -f - <<YAML
apiVersion: apps.kubeblocks.io/v1alpha1
kind: OpsRequest
metadata:
  name: ${ops}
  namespace: ${NAMESPACE}
spec:
  clusterName: ${CLUSTER}
  type: Switchover
  switchover:
  - componentName: ${COMPONENT}
    instanceName: "*"
YAML
  wait_ops "${ops}"
}

apply_fence() {
  local instance="$1"
  local ops="ha-drill-fence-$(date +%s)"
  kubectl apply -f - <<YAML
apiVersion: apps.kubeblocks.io/v1alpha1
kind: OpsRequest
metadata:
  name: ${ops}
  namespace: ${NAMESPACE}
spec:
  clusterName: ${CLUSTER}
  type: HorizontalScaling
  horizontalScaling:
  - componentName: ${COMPONENT}
    scaleIn:
      replicaChanges: 1
      onlineInstancesToOffline:
      - ${instance}
YAML
  wait_ops "${ops}"
}

apply_rejoin() {
  local instance="$1"
  local ops="ha-drill-rejoin-$(date +%s)"
  kubectl apply -f - <<YAML
apiVersion: apps.kubeblocks.io/v1alpha1
kind: OpsRequest
metadata:
  name: ${ops}
  namespace: ${NAMESPACE}
spec:
  clusterName: ${CLUSTER}
  type: HorizontalScaling
  horizontalScaling:
  - componentName: ${COMPONENT}
    scaleOut:
      replicaChanges: 1
      offlineInstancesToOnline:
      - ${instance}
YAML
  wait_ops "${ops}"
}

apply_rebuild() {
  local instance="$1"
  local ops="ha-drill-rebuild-$(date +%s)"
  kubectl apply -f - <<YAML
apiVersion: apps.kubeblocks.io/v1alpha1
kind: OpsRequest
metadata:
  name: ${ops}
  namespace: ${NAMESPACE}
spec:
  clusterName: ${CLUSTER}
  type: RebuildInstance
  force: true
  rebuildFrom:
  - componentName: ${COMPONENT}
    inPlace: false
    instances:
    - name: ${instance}
YAML
  wait_ops "${ops}"
}

apply_backup_restore_drill() {
  local stamp backup_ops restore_ops backup_name restore_cluster
  stamp="$(date +%s)"
  backup_name="${CLUSTER}-ha-drill-${stamp}"
  backup_ops="ha-drill-backup-${stamp}"
  restore_ops="ha-drill-restore-${stamp}"
  restore_cluster="${CLUSTER}-restore-${stamp}"

  kubectl apply -f - <<YAML
apiVersion: apps.kubeblocks.io/v1alpha1
kind: OpsRequest
metadata:
  name: ${backup_ops}
  namespace: ${NAMESPACE}
spec:
  clusterName: ${CLUSTER}
  type: Backup
  backup:
    backupName: ${backup_name}
    backupMethod: pg-basebackup
    deletionPolicy: ${DRILL_BACKUP_DELETION_POLICY}
    retentionPeriod: 168h
YAML
  wait_ops "${backup_ops}"

  kubectl apply -f - <<YAML
apiVersion: apps.kubeblocks.io/v1alpha1
kind: OpsRequest
metadata:
  name: ${restore_ops}
  namespace: ${NAMESPACE}
spec:
  clusterName: ${restore_cluster}
  type: Restore
  restore:
    backupName: ${backup_name}
    backupNamespace: ${NAMESPACE}
    volumeRestorePolicy: Parallel
    deferPostReadyUntilClusterRunning: true
YAML
  wait_ops "${restore_ops}"
  wait_for "restore cluster ${restore_cluster} Running" \
    "test \"\$(kubectl get cluster -n '${NAMESPACE}' '${restore_cluster}' -o jsonpath='{.status.phase}' 2>/dev/null)\" = Running"

  if [ "${DRILL_CLEANUP_RESTORE_CLUSTER}" = "true" ]; then
    kubectl delete cluster -n "${NAMESPACE}" "${restore_cluster}" --wait=false
    wait_for "restore cluster ${restore_cluster} deleted" \
      "test \"\$(kubectl get cluster -n '${NAMESPACE}' '${restore_cluster}' --ignore-not-found --no-headers 2>/dev/null | wc -l | tr -d ' ')\" = 0"
    wait_for "restore cluster ${restore_cluster} pods deleted" \
      "test \"\$(kubectl get pod -n '${NAMESPACE}' -l app.kubernetes.io/instance='${restore_cluster}' --no-headers 2>/dev/null | wc -l | tr -d ' ')\" = 0"
  fi

  if [ "${DRILL_CLEANUP_BACKUP}" = "true" ]; then
    if [ "${DRILL_BACKUP_DELETION_POLICY}" != "Delete" ]; then
      die "DRILL_CLEANUP_BACKUP=true requires DRILL_BACKUP_DELETION_POLICY=Delete"
    fi
    kubectl delete backup -n "${NAMESPACE}" "${backup_name}" --wait=false
    wait_for "backup ${backup_name} deleted" \
      "test \"\$(kubectl get backup -n '${NAMESPACE}' '${backup_name}' --ignore-not-found --no-headers 2>/dev/null | wc -l | tr -d ' ')\" = 0"
  fi
}

wait_cluster_running

old_primary="$(role_pod primary)"
[ -n "${old_primary}" ] || die "primary pod not found"
log "current primary: ${old_primary}"

log "running KB-native switchover with automatic candidate selection"
apply_switchover_auto
wait_cluster_running

new_primary="$(role_pod primary)"
[ -n "${new_primary}" ] || die "new primary pod not found"
[ "${new_primary}" != "${old_primary}" ] || die "switchover did not move primary from ${old_primary}"
log "new primary: ${new_primary}"

log "fencing old primary through HorizontalScaling/offlineInstances"
apply_fence "${old_primary}"
wait_for "fenced pod ${old_primary} deleted" \
  "! kubectl get pod -n '${NAMESPACE}' '${old_primary}' >/dev/null 2>&1"

log "rejoining fenced instance through HorizontalScaling/offlineInstancesToOnline"
apply_rejoin "${old_primary}"
wait_cluster_running

if [ "${WITH_REBUILD}" = "true" ]; then
  replica="$(role_pod secondary)"
  [ -n "${replica}" ] || die "secondary pod not found for rebuild"
  log "rebuilding replica ${replica} through RebuildInstance"
  apply_rebuild "${replica}"
  wait_cluster_running
fi

if [ "${WITH_BACKUP}" = "true" ]; then
  log "running backup and restore drill"
  apply_backup_restore_drill
fi

log "HA drill completed"

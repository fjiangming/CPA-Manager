import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { Input } from '@/components/ui/Input';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { IconChevronLeft, IconRefreshCw, IconX } from '@/components/ui/icons';
import { useNotificationStore } from '@/stores';
import {
  scheduledInspectionApi,
  type InspectionSchedule,
  type InspectionSchedulerStatus,
  type InspectionHistorySummary,
  type InspectionHistoryRecord,
  type InspectionAccountResult,
} from '@/services/api/scheduledInspection';
import styles from './ScheduledInspectionPage.module.scss';

function formatTime(ms: number, locale?: string): string {
  if (!ms) return '—';
  return new Date(ms).toLocaleString(locale || 'en', {
    month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  });
}

function formatDuration(ms: number): string {
  if (ms <= 0) return '—';
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  return `${m}m ${s % 60}s`;
}

export function ScheduledInspectionPage() {
  const { t, i18n } = useTranslation();
  const showNotification = useNotificationStore((state) => state.showNotification);
  const showConfirmation = useNotificationStore((state) => state.showConfirmation);

  const [_schedule, setSchedule] = useState<InspectionSchedule | null>(null);
  const [status, setStatus] = useState<InspectionSchedulerStatus | null>(null);
  const [history, setHistory] = useState<InspectionHistorySummary[]>([]);
  const [detail, setDetail] = useState<InspectionHistoryRecord | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [executing, setExecuting] = useState(false);
  const [runningNow, setRunningNow] = useState(false);

  // Draft form state
  const [draftEnabled, setDraftEnabled] = useState(false);
  const [draftInterval, setDraftInterval] = useState('6');
  const [draftAutoExecute, setDraftAutoExecute] = useState(false);
  const [draftThreshold, setDraftThreshold] = useState('100');
  const [draftWorkers, setDraftWorkers] = useState('4');
  const [draftSampleSize, setDraftSampleSize] = useState('0');
  const [draftRetries, setDraftRetries] = useState('0');
  const [draftTimeout, setDraftTimeout] = useState('15000');

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const [schedRes, histRes] = await Promise.all([
        scheduledInspectionApi.getSchedule(),
        scheduledInspectionApi.getHistory(100),
      ]);
      const sch = schedRes.schedule;
      setSchedule(sch);
      setStatus(schedRes.status);
      setHistory(histRes.records || []);
      setDraftEnabled(sch.enabled);
      setDraftInterval(String(sch.intervalHours || 6));
      setDraftAutoExecute(sch.autoExecute);
      setDraftThreshold(String(sch.usedPercentThreshold || 100));
      setDraftWorkers(String(sch.workers || 4));
      setDraftSampleSize(String(sch.sampleSize || 0));
      setDraftRetries(String(sch.retries || 0));
      setDraftTimeout(String(sch.timeoutMs || 15000));
    } catch {
      // silent - API may not be available
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  const handleSave = useCallback(async () => {
    setSaving(true);
    try {
      const payload: Partial<InspectionSchedule> = {
        enabled: draftEnabled,
        intervalHours: Math.max(1, Number(draftInterval) || 6),
        autoExecute: draftAutoExecute,
        usedPercentThreshold: Math.min(100, Math.max(0, Number(draftThreshold) || 100)),
        workers: Math.max(1, Number(draftWorkers) || 4),
        sampleSize: Math.max(0, Number(draftSampleSize) || 0),
        retries: Math.max(0, Number(draftRetries) || 0),
        timeoutMs: Math.max(1000, Number(draftTimeout) || 15000),
      };
      const res = await scheduledInspectionApi.updateSchedule(payload);
      setSchedule(res.schedule);
      setStatus(res.status);
      showNotification(t('monitoring.scheduled_inspection_saved'), 'success');
    } catch {
      showNotification(t('monitoring.scheduled_inspection_save_failed'), 'error');
    } finally {
      setSaving(false);
    }
  }, [draftAutoExecute, draftEnabled, draftInterval, draftRetries, draftSampleSize, draftThreshold, draftTimeout, draftWorkers, showNotification, t]);

  const handleRunNow = useCallback(async () => {
    setRunningNow(true);
    try {
      await scheduledInspectionApi.runNow();
      showNotification(t('monitoring.scheduled_inspection_run_now_started'), 'success');
      await loadData();
    } catch {
      showNotification(t('monitoring.scheduled_inspection_run_now_failed'), 'error');
    } finally {
      setRunningNow(false);
    }
  }, [loadData, showNotification, t]);

  const openDetail = useCallback(async (id: number) => {
    try {
      const res = await scheduledInspectionApi.getHistoryDetail(id);
      setDetail(res.record);
    } catch {
      showNotification('Failed to load detail', 'error');
    }
  }, [showNotification]);

  const handleExecute = useCallback(async () => {
    if (!detail) return;
    if (detail.executed) {
      showNotification(t('monitoring.scheduled_inspection_already_executed'), 'error');
      return;
    }
    showConfirmation({
      title: t('monitoring.scheduled_inspection_execute_btn'),
      message: t('monitoring.scheduled_inspection_execute_confirm'),
      confirmText: t('monitoring.scheduled_inspection_execute_btn'),
      cancelText: t('common.cancel'),
      variant: 'danger',
      onConfirm: async () => {
        setExecuting(true);
        try {
          const res = await scheduledInspectionApi.executeHistoryActions(detail.id);
          showNotification(
            t('monitoring.scheduled_inspection_execute_success', {
              success: res.success,
              failed: res.failed,
            }),
            'success'
          );
          setDetail(null);
          await loadData();
        } catch {
          showNotification(t('monitoring.scheduled_inspection_execute_failed'), 'error');
        } finally {
          setExecuting(false);
        }
      },
    });
  }, [detail, loadData, showConfirmation, showNotification, t]);

  const actionableCount = useMemo(() => {
    if (!detail?.accountResults) return 0;
    return detail.accountResults.filter(
      (r) => r.action === 'delete' || r.action === 'disable' || r.action === 'enable'
    ).length;
  }, [detail]);

  return (
    <div className={styles.page}>
      <div className={styles.pageHeader}>
        <h1 className={styles.pageTitle}>{t('monitoring.scheduled_inspection_title')}</h1>
        <p className={styles.description}>{t('monitoring.scheduled_inspection_desc')}</p>
      </div>

      <div className={styles.statusBar}>
        <div className={styles.schedulerStatus}>
          <span className={`${styles.statusDot} ${status?.running ? styles.running : styles.stopped}`} />
          <span style={{ fontSize: '0.85rem', fontWeight: 600 }}>
            {status?.running
              ? t('monitoring.scheduled_inspection_scheduler_running')
              : t('monitoring.scheduled_inspection_scheduler_stopped')}
          </span>
          {status?.nextRunAtMs ? (
            <span className={styles.statusMeta}>
              {t('monitoring.scheduled_inspection_next_run')}: {formatTime(status.nextRunAtMs, i18n.language)}
            </span>
          ) : null}
          {status?.lastRunAtMs ? (
            <span className={styles.statusMeta}>
              {t('monitoring.scheduled_inspection_last_run')}: {formatTime(status.lastRunAtMs, i18n.language)}
            </span>
          ) : null}
        </div>
        <div className={styles.statusActions}>
          <Link to="/monitoring/codex-inspection" className={styles.quickLink}>
            <IconChevronLeft size={14} />
            <span>{t('monitoring.scheduled_inspection_back')}</span>
          </Link>
        </div>
      </div>

      <Card className={styles.panel}>
        <h2 className={styles.panelTitle}>{t('monitoring.scheduled_inspection_schedule_title')}</h2>
        <p className={styles.panelDesc}>{t('monitoring.scheduled_inspection_desc')}</p>

        <div className={styles.formRow}>
          <ToggleSwitch checked={draftEnabled} onChange={setDraftEnabled} />
          <span style={{ fontSize: '0.85rem' }}>{t('monitoring.scheduled_inspection_enabled')}</span>
        </div>

        <div className={styles.formGrid}>
          <div>
            <div className={styles.formLabel}>{t('monitoring.scheduled_inspection_interval')}</div>
            <Input value={draftInterval} onChange={(e) => setDraftInterval(e.target.value)} type="number" />
          </div>
          <div>
            <div className={styles.formLabel}>{t('monitoring.scheduled_inspection_threshold')}</div>
            <Input value={draftThreshold} onChange={(e) => setDraftThreshold(e.target.value)} type="number" />
          </div>
          <div>
            <div className={styles.formLabel}>{t('monitoring.scheduled_inspection_workers')}</div>
            <Input value={draftWorkers} onChange={(e) => setDraftWorkers(e.target.value)} type="number" />
          </div>
          <div>
            <div className={styles.formLabel}>{t('monitoring.scheduled_inspection_sample_size')}</div>
            <Input value={draftSampleSize} onChange={(e) => setDraftSampleSize(e.target.value)} type="number" />
          </div>
          <div>
            <div className={styles.formLabel}>{t('monitoring.scheduled_inspection_retries')}</div>
            <Input value={draftRetries} onChange={(e) => setDraftRetries(e.target.value)} type="number" />
          </div>
          <div>
            <div className={styles.formLabel}>{t('monitoring.scheduled_inspection_timeout')}</div>
            <Input value={draftTimeout} onChange={(e) => setDraftTimeout(e.target.value)} type="number" />
          </div>
        </div>

        <div className={styles.formRow}>
          <ToggleSwitch checked={draftAutoExecute} onChange={setDraftAutoExecute} />
          <div>
            <span style={{ fontSize: '0.85rem' }}>{t('monitoring.scheduled_inspection_auto_execute')}</span>
            <div className={styles.formHint}>{t('monitoring.scheduled_inspection_auto_execute_hint')}</div>
          </div>
        </div>

        <div className={styles.buttonRow}>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? '...' : t('monitoring.scheduled_inspection_save')}
          </Button>
          <Button onClick={handleRunNow} variant="secondary" disabled={runningNow}>
            <IconRefreshCw size={14} />
            {runningNow ? '...' : t('monitoring.scheduled_inspection_run_now')}
          </Button>
        </div>
      </Card>

      <Card className={styles.panel}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
          <h2 className={styles.panelTitle}>{t('monitoring.scheduled_inspection_history_title')}</h2>
          <Button variant="ghost" onClick={loadData} disabled={loading}>
            <IconRefreshCw size={14} />
          </Button>
        </div>

        {history.length === 0 ? (
          <div className={styles.emptyState}>{t('monitoring.scheduled_inspection_history_empty')}</div>
        ) : (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>{t('monitoring.scheduled_inspection_col_time')}</th>
                <th>{t('monitoring.scheduled_inspection_col_trigger')}</th>
                <th>{t('monitoring.scheduled_inspection_col_probed')}</th>
                <th>{t('monitoring.scheduled_inspection_col_actions')}</th>
                <th>{t('monitoring.scheduled_inspection_col_executed')}</th>
              </tr>
            </thead>
            <tbody>
              {history.map((row) => (
                <tr key={row.id} className={styles.tableRow} onClick={() => openDetail(row.id)}>
                  <td>
                    <div>{formatTime(row.startedAtMs, i18n.language)}</div>
                    <div style={{ fontSize: '0.72rem', color: 'var(--text-secondary)' }}>
                      {formatDuration(row.finishedAtMs - row.startedAtMs)}
                    </div>
                  </td>
                  <td>
                    <span className={`${styles.badge} ${row.trigger === 'scheduled' ? styles.scheduled : styles.manual}`}>
                      {row.trigger === 'scheduled'
                        ? t('monitoring.scheduled_inspection_trigger_scheduled')
                        : t('monitoring.scheduled_inspection_trigger_manual')}
                    </span>
                  </td>
                  <td>{row.probedAccounts} / {row.totalAccounts}</td>
                  <td>
                    <div className={styles.actionSummary}>
                      {row.deleteCount > 0 && <span className={`${styles.actionChip} ${styles.chipDelete}`}>D:{row.deleteCount}</span>}
                      {row.disableCount > 0 && <span className={`${styles.actionChip} ${styles.chipDisable}`}>-:{row.disableCount}</span>}
                      {row.enableCount > 0 && <span className={`${styles.actionChip} ${styles.chipEnable}`}>+:{row.enableCount}</span>}
                      {row.deleteCount === 0 && row.disableCount === 0 && row.enableCount === 0 && <span>—</span>}
                    </div>
                  </td>
                  <td>
                    <span className={`${styles.badge} ${row.executed ? styles.executed : styles.pending}`}>
                      {row.executed ? `✓ ${row.executeSuccess}/${row.executeSuccess + row.executeFailed}` : '—'}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      {detail && (
        <div className={styles.detailOverlay} onClick={() => setDetail(null)}>
          <div className={styles.detailPanel} onClick={(e) => e.stopPropagation()}>
            <div className={styles.detailHeader}>
              <h2 className={styles.panelTitle}>{t('monitoring.scheduled_inspection_detail_title')}</h2>
              <div className={styles.buttonRow}>
                {!detail.executed && actionableCount > 0 && (
                  <Button onClick={handleExecute} disabled={executing} variant="danger">
                    {executing ? '...' : t('monitoring.scheduled_inspection_execute_btn')}
                  </Button>
                )}
                <Button variant="ghost" onClick={() => setDetail(null)}>
                  <IconX size={16} />
                </Button>
              </div>
            </div>

            <div className={styles.detailSummary}>
              <div className={styles.detailStat}>
                <span className={styles.detailStatLabel}>{t('monitoring.scheduled_inspection_col_time')}</span>
                <span className={styles.detailStatValue}>{formatTime(detail.startedAtMs, i18n.language)}</span>
              </div>
              <div className={styles.detailStat}>
                <span className={styles.detailStatLabel}>{t('monitoring.scheduled_inspection_col_probed')}</span>
                <span className={styles.detailStatValue}>{detail.probedAccounts} / {detail.totalAccounts}</span>
              </div>
              <div className={styles.detailStat}>
                <span className={styles.detailStatLabel}>{t('monitoring.codex_inspection_delete_count')}</span>
                <span className={styles.detailStatValue}>{detail.deleteCount}</span>
              </div>
              <div className={styles.detailStat}>
                <span className={styles.detailStatLabel}>{t('monitoring.codex_inspection_disable_count')}</span>
                <span className={styles.detailStatValue}>{detail.disableCount}</span>
              </div>
              <div className={styles.detailStat}>
                <span className={styles.detailStatLabel}>{t('monitoring.codex_inspection_enable_count')}</span>
                <span className={styles.detailStatValue}>{detail.enableCount}</span>
              </div>
              <div className={styles.detailStat}>
                <span className={styles.detailStatLabel}>{t('monitoring.scheduled_inspection_col_executed')}</span>
                <span className={styles.detailStatValue}>
                  {detail.executed ? `✓ ${detail.executeSuccess}/${detail.executeSuccess + detail.executeFailed}` : '—'}
                </span>
              </div>
            </div>

            {detail.accountResults && detail.accountResults.length > 0 ? (
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>{t('monitoring.codex_inspection_file_name')}</th>
                    <th>{t('monitoring.codex_inspection_current_state')}</th>
                    <th>{t('monitoring.codex_inspection_http_status')}</th>
                    <th>{t('monitoring.codex_inspection_used_percent')}</th>
                    <th>{t('monitoring.codex_inspection_next_action')}</th>
                    <th>{t('monitoring.codex_inspection_reason')}</th>
                  </tr>
                </thead>
                <tbody>
                  {detail.accountResults.map((r: InspectionAccountResult) => (
                    <tr key={r.key}>
                      <td>
                        <div style={{ fontWeight: 500 }}>{r.displayAccount}</div>
                        <div style={{ fontSize: '0.72rem', color: 'var(--text-secondary)' }}>{r.fileName}</div>
                      </td>
                      <td>
                        {r.disabled
                          ? t('monitoring.codex_inspection_state_disabled')
                          : t('monitoring.codex_inspection_state_enabled')}
                      </td>
                      <td>{r.statusCode ?? '—'}</td>
                      <td>{r.usedPercent != null ? `${r.usedPercent.toFixed(1)}%` : '—'}</td>
                      <td>
                        <ActionBadge action={r.action} t={t} />
                      </td>
                      <td style={{ fontSize: '0.78rem' }}>{r.actionReason || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <div className={styles.emptyState}>{t('monitoring.codex_inspection_empty')}</div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function ActionBadge({ action, t }: { action: string; t: (key: string) => string }) {
  const classMap: Record<string, string> = {
    delete: styles.chipDelete,
    disable: styles.chipDisable,
    enable: styles.chipEnable,
  };
  const labelMap: Record<string, string> = {
    delete: t('monitoring.codex_inspection_action_delete'),
    disable: t('monitoring.codex_inspection_action_disable'),
    enable: t('monitoring.codex_inspection_action_enable'),
    keep: t('monitoring.codex_inspection_action_keep'),
  };
  return (
    <span className={`${styles.actionChip} ${classMap[action] || ''}`}>
      {labelMap[action] || action}
    </span>
  );
}

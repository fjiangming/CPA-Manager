/**
 * Scheduled inspection API client.
 */

import { apiClient } from './client';

export interface InspectionSchedule {
  enabled: boolean;
  intervalHours: number;
  targetType: string;
  workers: number;
  deleteWorkers: number;
  timeoutMs: number;
  retries: number;
  usedPercentThreshold: number;
  sampleSize: number;
  userAgent: string;
  autoExecute: boolean;
  updatedAtMs: number;
}

export interface InspectionSchedulerStatus {
  running: boolean;
  lastRunAtMs: number;
  nextRunAtMs: number;
}

export interface InspectionAccountResult {
  key: string;
  fileName: string;
  displayAccount: string;
  authIndex: string;
  accountId: string;
  provider: string;
  disabled: boolean;
  statusCode: number | null;
  usedPercent: number | null;
  isQuota: boolean;
  action: 'keep' | 'delete' | 'disable' | 'enable';
  actionReason: string;
  error?: string;
  executed: boolean;
  executeSuccess: boolean;
  executeError?: string;
}

export interface InspectionHistorySummary {
  id: number;
  trigger: string;
  startedAtMs: number;
  finishedAtMs: number;
  totalAccounts: number;
  probedAccounts: number;
  deleteCount: number;
  disableCount: number;
  enableCount: number;
  keepCount: number;
  executed: boolean;
  executeSuccess: number;
  executeFailed: number;
  error?: string;
}

export interface InspectionHistoryRecord extends InspectionHistorySummary {
  accountResults: InspectionAccountResult[];
  schedule: InspectionSchedule;
}

export const scheduledInspectionApi = {
  getSchedule: () =>
    apiClient.get<{ schedule: InspectionSchedule; status: InspectionSchedulerStatus }>(
      '/inspection/schedule'
    ),

  updateSchedule: (schedule: Partial<InspectionSchedule>) =>
    apiClient.put<{ ok: boolean; schedule: InspectionSchedule; status: InspectionSchedulerStatus }>(
      '/inspection/schedule',
      schedule
    ),

  getHistory: (limit?: number) =>
    apiClient.get<{ records: InspectionHistorySummary[] }>('/inspection/history', {
      params: limit ? { limit } : undefined,
    }),

  getHistoryDetail: (id: number) =>
    apiClient.get<{ record: InspectionHistoryRecord }>(`/inspection/history/${id}`),

  executeHistoryActions: (id: number) =>
    apiClient.post<{ success: number; failed: number }>(`/inspection/history/${id}/execute`),

  runNow: () =>
    apiClient.post<{ record: InspectionHistoryRecord }>('/inspection/run'),
};

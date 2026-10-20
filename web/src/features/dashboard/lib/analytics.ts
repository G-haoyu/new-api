/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import {
  AVAILABILITY_MIN_SAMPLES,
  MODEL_AVAILABILITY_STATUSES,
} from '@/features/dashboard/constants'
import type {
  ModelAvailabilityCounts,
  ModelAvailabilityRow,
  ModelAvailabilityStatus,
  RankedModelAvailabilityRow,
  UsageMetrics,
} from '@/features/dashboard/types'

// z-score for a 95% confidence interval.
const WILSON_Z_95 = 1.96

/** Success percentage over tracked requests; null when no outcomes exist. */
export function successRate(
  metrics: Pick<
    UsageMetrics,
    'tracked_count' | 'stream_error_count' | 'failed_count'
  >
): number | null {
  const denominator = metrics.tracked_count + metrics.failed_count
  if (denominator <= 0) return null
  return (
    ((metrics.tracked_count - metrics.stream_error_count) / denominator) * 100
  )
}

/** Cache-read tokens as a percentage of normalized input; null without input. */
export function cacheHitRate(
  metrics: Pick<UsageMetrics, 'cache_read_tokens' | 'input_tokens'>
): number | null {
  if (metrics.input_tokens <= 0) return null
  return (metrics.cache_read_tokens / metrics.input_tokens) * 100
}

/** Mean seconds for successful, uninterrupted requests; null without samples. */
export function avgUseTime(
  metrics: Pick<UsageMetrics, 'use_time_total' | 'use_time_count'>
): number | null {
  if (metrics.use_time_count <= 0) return null
  return metrics.use_time_total / metrics.use_time_count
}

/** Average RPM/TPM using a minimum one-minute duration. */
export function ratePerMinute(value: number, minutes: number): number {
  return value / Math.max(minutes, 1)
}

export function availabilityTotal(counts: ModelAvailabilityCounts): number {
  return counts.success_count + counts.stream_error_count + counts.failed_count
}

/** Successful requests as a percentage of all outcomes; null without samples. */
export function availabilityRate(
  counts: ModelAvailabilityCounts
): number | null {
  const total = availabilityTotal(counts)
  if (total <= 0) return null
  return (counts.success_count / total) * 100
}

/** Wilson lower bound (0-1), accounting for uncertainty in small samples. */
export function wilsonLowerBound(
  successes: number,
  total: number,
  z: number = WILSON_Z_95
): number {
  if (total <= 0 || successes <= 0) return 0
  const p = successes / total
  const z2 = z * z
  const center = p + z2 / (2 * total)
  const margin = z * Math.sqrt((p * (1 - p)) / total + z2 / (4 * total * total))
  return (center - margin) / (1 + z2 / total)
}

export function availabilityStatus(
  counts: ModelAvailabilityCounts
): ModelAvailabilityStatus {
  const rate = availabilityRate(counts)
  if (availabilityTotal(counts) < AVAILABILITY_MIN_SAMPLES || rate === null) {
    return MODEL_AVAILABILITY_STATUSES.insufficient_samples
  }
  if (rate >= 99) return MODEL_AVAILABILITY_STATUSES.stable
  if (rate >= 95) return MODEL_AVAILABILITY_STATUSES.fair
  if (rate >= 90) return MODEL_AVAILABILITY_STATUSES.degraded
  return MODEL_AVAILABILITY_STATUSES.unstable
}

/** Rank by Wilson score, breaking ties by sample count and model name. */
export function rankAvailability(
  rows: ModelAvailabilityRow[],
  minutes: number
): RankedModelAvailabilityRow[] {
  const grandTotal = rows.reduce((sum, row) => sum + availabilityTotal(row), 0)

  return rows
    .map((row) => {
      const total = availabilityTotal(row)
      return {
        ...row,
        total,
        rate: availabilityRate(row),
        wilson: wilsonLowerBound(row.success_count, total) * 100,
        share: grandTotal > 0 ? (total / grandTotal) * 100 : 0,
        rpm: ratePerMinute(total, minutes),
        tpm: ratePerMinute(row.total_tokens, minutes),
        status: availabilityStatus(row),
      }
    })
    .sort((a, b) => {
      if (b.wilson !== a.wilson) return b.wilson - a.wilson
      if (b.total !== a.total) return b.total - a.total
      return a.model_name.localeCompare(b.model_name)
    })
    .map((row, index) => ({ ...row, rank: index + 1 }))
}

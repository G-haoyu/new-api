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
import type {
  UsageMetrics,
} from '@/features/dashboard/types'

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

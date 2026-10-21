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
import { describe, expect, test } from 'vitest'

import type { ModelAvailabilityRow } from '../../types'
import {
  availabilityRate,
  availabilityStatus,
  availabilityTotal,
  avgUseTime,
  cacheHitRate,
  rankAvailability,
  ratePerMinute,
  successRate,
  wilsonLowerBound,
} from '../analytics'

function availabilityRow(
  modelName: string,
  success: number,
  streamError: number,
  failed: number,
  totalTokens = 0
): ModelAvailabilityRow {
  return {
    model_name: modelName,
    success_count: success,
    stream_error_count: streamError,
    failed_count: failed,
    total_tokens: totalTokens,
    last_called_at: 1_752_000_000,
  }
}

describe('successRate', () => {
  test('counts stream errors and final failures against tracked requests', () => {
    const rate = successRate({
      tracked_count: 100,
      stream_error_count: 2,
      failed_count: 3,
    })

    expect(rate).toBeCloseTo(95.145631068, 8)
  })

  test('returns null when no tracked request and no failure exist', () => {
    expect(
      successRate({ tracked_count: 0, stream_error_count: 0, failed_count: 0 })
    ).toBeNull()
  })

  test('returns 0 when only final failures were recorded', () => {
    expect(
      successRate({ tracked_count: 0, stream_error_count: 0, failed_count: 4 })
    ).toBe(0)
  })
})

describe('cacheHitRate', () => {
  test('divides cache-read tokens by input tokens', () => {
    expect(cacheHitRate({ cache_read_tokens: 250, input_tokens: 1000 })).toBe(
      25
    )
  })

  test('returns null when there were no input tokens', () => {
    expect(cacheHitRate({ cache_read_tokens: 0, input_tokens: 0 })).toBeNull()
  })
})

describe('avgUseTime', () => {
  test('divides total seconds by the number of timed requests', () => {
    expect(avgUseTime({ use_time_total: 30, use_time_count: 12 })).toBe(2.5)
  })

  test('returns null when no request duration was recorded', () => {
    expect(avgUseTime({ use_time_total: 0, use_time_count: 0 })).toBeNull()
  })
})

describe('ratePerMinute', () => {
  test('divides the value by the window length in minutes', () => {
    expect(ratePerMinute(600, 10)).toBe(60)
  })

  test('treats windows shorter than one minute as one minute', () => {
    expect(ratePerMinute(5, 0)).toBe(5)
  })
})

describe('availability counts', () => {
  test('total adds successes, stream errors and final failures', () => {
    expect(availabilityTotal(availabilityRow('m', 90, 6, 4))).toBe(100)
  })

  test('rate is the success share of all requests', () => {
    expect(availabilityRate(availabilityRow('m', 90, 6, 4))).toBe(90)
  })

  test('rate is null for a window without requests', () => {
    expect(availabilityRate(availabilityRow('m', 0, 0, 0))).toBeNull()
  })
})

describe('wilsonLowerBound', () => {
  test('returns 0 for an empty sample', () => {
    expect(wilsonLowerBound(0, 0)).toBe(0)
  })

  test('returns 0 when nothing succeeded', () => {
    expect(wilsonLowerBound(0, 20)).toBe(0)
  })

  test('keeps a perfect small sample well below 1', () => {
    expect(wilsonLowerBound(10, 10)).toBeCloseTo(0.7224598312, 9)
  })

  test('computes the 95% lower bound for 95 of 100', () => {
    expect(wilsonLowerBound(95, 100)).toBeCloseTo(0.8882480347, 9)
  })
})

describe('availabilityStatus', () => {
  test.each([
    { counts: [9, 0, 0], expected: 'insufficient_samples' },
    { counts: [0, 0, 0], expected: 'insufficient_samples' },
    { counts: [99, 1, 0], expected: 'stable' },
    { counts: [98, 1, 1], expected: 'fair' },
    { counts: [95, 0, 5], expected: 'fair' },
    { counts: [90, 5, 5], expected: 'degraded' },
    { counts: [89, 6, 5], expected: 'unstable' },
  ])(
    'success/stream error/failed $counts grades as $expected',
    ({ counts, expected }) => {
      const [success, streamError, failed] = counts
      const status = availabilityStatus(
        availabilityRow('m', success, streamError, failed)
      )

      expect(status.id).toBe(expected)
    }
  )

  test('exposes an English i18n label key with the grade', () => {
    expect(availabilityStatus(availabilityRow('m', 99, 1, 0))).toEqual({
      id: 'stable',
      labelKey: 'Stable',
    })
  })
})

describe('rankAvailability', () => {
  test('orders by Wilson lower bound, then total requests', () => {
    const ranked = rankAvailability(
      [
        availabilityRow('few-failures', 0, 0, 5),
        availabilityRow('small-perfect', 10, 0, 0),
        availabilityRow('many-failures', 0, 0, 20),
        availabilityRow('large-stable', 99, 1, 0),
      ],
      10
    )

    expect(ranked.map((row) => [row.rank, row.model_name])).toEqual([
      [1, 'large-stable'],
      [2, 'small-perfect'],
      [3, 'many-failures'],
      [4, 'few-failures'],
    ])
  })

  test('breaks full ties by model name ascending', () => {
    const ranked = rankAvailability(
      [
        availabilityRow('model-b', 50, 0, 0),
        availabilityRow('model-a', 50, 0, 0),
      ],
      10
    )

    expect(ranked.map((row) => row.model_name)).toEqual(['model-a', 'model-b'])
  })

  test('derives rate, Wilson percentage, share, RPM, TPM and status per row', () => {
    const ranked = rankAvailability(
      [
        availabilityRow('stable-model', 95, 3, 2, 6000),
        availabilityRow('quiet-model', 0, 0, 0, 0),
        availabilityRow('other-model', 300, 0, 0, 0),
      ],
      10
    )
    const stable = ranked.find((row) => row.model_name === 'stable-model')
    const quiet = ranked.find((row) => row.model_name === 'quiet-model')

    expect(stable).toMatchObject({
      total: 100,
      rate: 95,
      share: 25,
      rpm: 10,
      tpm: 600,
      status: { id: 'fair', labelKey: 'Fair' },
    })
    expect(stable?.wilson).toBeCloseTo(88.82480347, 7)
    expect(quiet).toMatchObject({
      total: 0,
      rate: null,
      wilson: 0,
      share: 0,
      rpm: 0,
      status: { id: 'insufficient_samples' },
    })
  })

  test('returns an empty list when the window has no models', () => {
    expect(rankAvailability([], 10)).toEqual([])
  })
})

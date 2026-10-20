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

import {
  avgUseTime,
  cacheHitRate,
  ratePerMinute,
  successRate,
} from '../analytics'

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

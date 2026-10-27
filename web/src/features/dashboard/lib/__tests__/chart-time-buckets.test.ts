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
import { afterAll, beforeAll, describe, expect, test } from 'vitest'

import { getChartBucketStart } from '@/lib/time'

import type { QuotaDataItem } from '../../types'
import { processChartData, processUserChartData } from '../charts'

// Match browser-local bucket boundaries.
function localTs(
  year: number,
  month: number,
  day: number,
  hour = 0,
  minute = 0
): number {
  return new Date(year, month - 1, day, hour, minute).getTime() / 1000
}

function usageRow(createdAt: number, count: number): QuotaDataItem {
  return {
    model_name: 'gpt-4o',
    username: 'alice',
    created_at: createdAt,
    count,
    quota: count * 100,
    token_used: count * 10,
  }
}

function callTrend(
  data: QuotaDataItem[],
  granularity: 'hour' | 'day' | 'week'
) {
  const spec = processChartData(data, granularity).spec_model_line
  const values = spec.data[0].values as Array<{ Time: string; Count: number }>
  return values.map((item) => [item.Time, item.Count])
}

describe('processChartData time buckets', () => {
  test('week granularity merges rows of the same Monday-start week into one bucket', () => {
    const data = [
      usageRow(localTs(2025, 7, 15, 10), 2), // Tuesday
      usageRow(localTs(2025, 7, 20, 23), 3), // Sunday, same week
      usageRow(localTs(2025, 7, 21, 0, 30), 4), // Monday, next week
    ]

    expect(callTrend(data, 'week')).toEqual([
      ['06-09 - 06-15', 0],
      ['06-16 - 06-22', 0],
      ['06-23 - 06-29', 0],
      ['06-30 - 07-06', 0],
      ['07-07 - 07-13', 0],
      ['07-14 - 07-20', 5],
      ['07-21 - 07-27', 4],
    ])
  })

  test('week buckets spanning Dec to Jan are ordered chronologically and labeled with the year', () => {
    const data = [
      usageRow(localTs(2024, 12, 20, 9), 1), // week of Mon 2024-12-16
      usageRow(localTs(2024, 12, 31, 9), 2), // week of Mon 2024-12-30
      usageRow(localTs(2025, 1, 2, 9), 3), // same week, next year
      usageRow(localTs(2025, 1, 8, 9), 4), // week of Mon 2025-01-06
    ]

    expect(callTrend(data, 'week')).toEqual([
      ['2024-11-25 - 12-01', 0],
      ['2024-12-02 - 12-08', 0],
      ['2024-12-09 - 12-15', 0],
      ['2024-12-16 - 12-22', 1],
      ['2024-12-23 - 12-29', 0],
      ['2024-12-30 - 01-05', 5],
      ['2025-01-06 - 01-12', 4],
    ])
  })

  test('day granularity spanning Dec to Jan keeps chronological order in every series', () => {
    const data = [
      usageRow(localTs(2024, 12, 30, 10), 1),
      usageRow(localTs(2024, 12, 31, 10), 2),
      usageRow(localTs(2025, 1, 1, 10), 3),
      usageRow(localTs(2025, 1, 2, 10), 4),
    ]

    const expected = [
      ['2024-12-27', 0],
      ['2024-12-28', 0],
      ['2024-12-29', 0],
      ['2024-12-30', 1],
      ['2024-12-31', 2],
      ['2025-01-01', 3],
      ['2025-01-02', 4],
    ]
    const barValues = processChartData(data, 'day').spec_line.data[0]
      .values as Array<{ Time: string; rawQuota: number }>

    expect(callTrend(data, 'day')).toEqual(expected)
    expect(barValues.map((item) => [item.Time, item.rawQuota / 100])).toEqual(
      expected
    )
  })

  test('hour granularity within one year keeps compact MM-DD HH:00 labels', () => {
    const data = [
      usageRow(localTs(2025, 7, 15, 10, 5), 1),
      usageRow(localTs(2025, 7, 15, 10, 40), 2),
      usageRow(localTs(2025, 7, 15, 12, 0), 5),
    ]

    expect(callTrend(data, 'hour')).toEqual([
      ['07-15 06:00', 0],
      ['07-15 07:00', 0],
      ['07-15 08:00', 0],
      ['07-15 09:00', 0],
      ['07-15 10:00', 3],
      ['07-15 11:00', 0],
      ['07-15 12:00', 5],
    ])
  })

  test('week rows older than the padded window stay on the axis', () => {
    const data = [
      usageRow(localTs(2025, 5, 5, 9), 1), // 11 weeks before the latest row
      usageRow(localTs(2025, 7, 21, 9), 2),
    ]

    expect(callTrend(data, 'week')).toEqual([
      ['05-05 - 05-11', 1],
      ['06-09 - 06-15', 0],
      ['06-16 - 06-22', 0],
      ['06-23 - 06-29', 0],
      ['06-30 - 07-06', 0],
      ['07-07 - 07-13', 0],
      ['07-14 - 07-20', 0],
      ['07-21 - 07-27', 2],
    ])
  })
})

describe('processUserChartData time buckets', () => {
  test('user trend merges week rows by Monday start across Dec to Jan', () => {
    const data = [
      usageRow(localTs(2024, 12, 31, 9), 1),
      usageRow(localTs(2025, 1, 2, 9), 2),
      usageRow(localTs(2025, 1, 7, 9), 4),
    ]

    const spec = processUserChartData(data, 'week').spec_user_trend
    const values = spec.data[0].values as Array<{
      Time: string
      rawQuota: number
    }>

    expect(values.map((item) => [item.Time, item.rawQuota])).toEqual([
      ['2024-12-30 - 01-05', 300],
      ['2025-01-06 - 01-12', 400],
    ])
  })
})

// Santiago skips midnight on 2024-09-08; build fixtures after switching TZ.
describe('week buckets across a midnight DST start (America/Santiago)', () => {
  const originalTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone

  beforeAll(() => {
    process.env.TZ = 'America/Santiago'
  })

  afterAll(() => {
    process.env.TZ = originalTimeZone
  })

  test('Wednesday and Sunday rows of each week share the Monday 00:00 bucket start', () => {
    const weeks = [
      { monday: localTs(2024, 8, 26), wed: localTs(2024, 8, 28, 12) },
      { monday: localTs(2024, 9, 2), wed: localTs(2024, 9, 4, 12) },
      { monday: localTs(2024, 9, 9), wed: localTs(2024, 9, 11, 12) },
    ]
    const sundays = [
      localTs(2024, 9, 1, 12),
      localTs(2024, 9, 8, 12), // DST start day
      localTs(2024, 9, 15, 12),
    ]

    expect(
      weeks.map((week, index) => [
        getChartBucketStart(week.wed, 'week'),
        getChartBucketStart(sundays[index], 'week'),
      ])
    ).toEqual(weeks.map((week) => [week.monday, week.monday]))
  })

  test('rows from the DST start week merge into a single chart bucket', () => {
    const data = [
      usageRow(localTs(2024, 9, 4, 12), 2), // Wednesday
      usageRow(localTs(2024, 9, 8, 12), 3), // Sunday, DST start day
      usageRow(localTs(2024, 9, 9, 0, 30), 4), // Monday, next week
    ]

    expect(callTrend(data, 'week')).toEqual([
      ['07-29 - 08-04', 0],
      ['08-05 - 08-11', 0],
      ['08-12 - 08-18', 0],
      ['08-19 - 08-25', 0],
      ['08-26 - 09-01', 0],
      ['09-02 - 09-08', 5],
      ['09-09 - 09-15', 4],
    ])
  })
})

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

import { buildPeakTpmSeries } from '../peak-tpm'

function localTs(
  year: number,
  month: number,
  day: number,
  hour = 0,
  minute = 0
): number {
  return new Date(year, month - 1, day, hour, minute).getTime() / 1000
}

describe('buildPeakTpmSeries', () => {
  test('hours without traffic inside the range are filled with zero', () => {
    const range = {
      start_timestamp: localTs(2025, 7, 31, 10, 30),
      end_timestamp: localTs(2025, 7, 31, 13, 30),
    }

    const series = buildPeakTpmSeries(
      [
        { hour: localTs(2025, 7, 31, 11), peak_tpm: 500 },
        { hour: localTs(2025, 7, 31, 13), peak_tpm: 900 },
      ],
      range,
      localTs(2025, 7, 31, 20)
    )

    expect(series.map((point) => [point.label, point.peak_tpm])).toEqual([
      ['07-31 10:00', 0],
      ['07-31 11:00', 500],
      ['07-31 12:00', 0],
      ['07-31 13:00', 900],
    ])
  })

  test('a range ending in the future stops at the current hour', () => {
    const range = {
      start_timestamp: localTs(2025, 7, 31, 10),
      end_timestamp: localTs(2025, 7, 31, 23),
    }

    const series = buildPeakTpmSeries(
      [{ hour: localTs(2025, 7, 31, 10), peak_tpm: 42 }],
      range,
      localTs(2025, 7, 31, 12, 15)
    )

    expect(series.map((point) => point.label)).toEqual([
      '07-31 10:00',
      '07-31 11:00',
      '07-31 12:00',
    ])
  })

  test('no traffic in the range returns an empty series', () => {
    const series = buildPeakTpmSeries(
      [],
      {
        start_timestamp: localTs(2025, 7, 31, 10),
        end_timestamp: localTs(2025, 7, 31, 13),
      },
      localTs(2025, 7, 31, 20)
    )

    expect(series).toEqual([])
  })

  test('a range spanning two years labels hours with the year', () => {
    const range = {
      start_timestamp: localTs(2024, 12, 31, 23),
      end_timestamp: localTs(2025, 1, 1, 0, 30),
    }

    const series = buildPeakTpmSeries(
      [{ hour: localTs(2025, 1, 1, 0), peak_tpm: 7 }],
      range,
      localTs(2025, 1, 2)
    )

    expect(series.map((point) => [point.label, point.peak_tpm])).toEqual([
      ['2024-12-31 23:00', 0],
      ['2025-01-01 00:00', 7],
    ])
  })
})

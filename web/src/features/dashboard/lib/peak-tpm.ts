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
import type { ILineChartSpec } from '@visactor/vchart'

import type { PeakTpmPoint } from '@/features/dashboard/types'
import dayjs from '@/lib/dayjs'
import { formatCompactNumber, formatNumber } from '@/lib/format'

const HOUR_SECONDS = 3600

export interface PeakTpmChartPoint {
  // Unix seconds at the start of the hour.
  hour: number
  label: string
  peak_tpm: number
}

/** Fill idle hours with zero through `now`; preserve an empty API result. */
export function buildPeakTpmSeries(
  points: PeakTpmPoint[],
  range: { start_timestamp: number; end_timestamp: number },
  now: number
): PeakTpmChartPoint[] {
  if (points.length === 0) return []

  const peakByHour = new Map<number, number>()
  for (const point of points) {
    peakByHour.set(point.hour, point.peak_tpm)
  }

  // Align padding with the API's hour boundaries.
  const anchor = points[0].hour
  const firstHour =
    anchor -
    Math.ceil((anchor - range.start_timestamp) / HOUR_SECONDS) * HOUR_SECONDS
  const gridEnd = Math.min(range.end_timestamp, now)
  const hours = new Set(peakByHour.keys())
  for (let hour = firstHour; hour <= gridEnd; hour += HOUR_SECONDS) {
    hours.add(hour)
  }

  const sorted = [...hours].sort((a, b) => a - b)
  const firstYear = dayjs(sorted[0] * 1000).year()
  const lastYear = dayjs((sorted.at(-1) ?? sorted[0]) * 1000).year()
  const labelFormat =
    firstYear === lastYear ? 'MM-DD HH:mm' : 'YYYY-MM-DD HH:mm'

  return sorted.map((hour) => ({
    hour,
    label: dayjs(hour * 1000).format(labelFormat),
    peak_tpm: peakByHour.get(hour) ?? 0,
  }))
}

export function buildPeakTpmSpec(
  series: PeakTpmChartPoint[],
  valueLabel: string
): ILineChartSpec {
  return {
    type: 'line',
    data: [{ id: 'peakTpmData', values: series }],
    xField: 'label',
    yField: 'peak_tpm',
    point: { visible: series.length === 1 },
    line: { style: { lineWidth: 2, curveType: 'monotone' } },
    axes: [
      {
        orient: 'bottom',
        type: 'band',
        label: { autoHide: true, autoLimit: true },
      },
      {
        orient: 'left',
        type: 'linear',
        label: {
          formatMethod: (value) => formatCompactNumber(Number(value)),
        },
      },
    ],
    tooltip: {
      mark: {
        title: { value: (datum) => String(datum?.label ?? '') },
        content: [
          {
            key: valueLabel,
            value: (datum) => formatNumber(Number(datum?.peak_tpm) || 0),
          },
        ],
      },
      dimension: {
        title: { value: (datum) => String(datum?.label ?? '') },
        content: [
          {
            key: valueLabel,
            value: (datum) => formatNumber(Number(datum?.peak_tpm) || 0),
          },
        ],
      },
    },
    background: 'transparent',
    animation: true,
  }
}

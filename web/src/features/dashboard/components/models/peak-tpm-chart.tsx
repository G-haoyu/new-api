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
import { useQuery } from '@tanstack/react-query'
import { VChart } from '@visactor/react-vchart'
import { Activity } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { IconBadge } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { getPeakTpm } from '@/features/dashboard/api'
import { getDefaultDays } from '@/features/dashboard/lib'
import {
  buildPeakTpmSeries,
  buildPeakTpmSpec,
} from '@/features/dashboard/lib/peak-tpm'
import type {
  DashboardFilters,
  PeakTpmParams,
} from '@/features/dashboard/types'
import { formatNumber } from '@/lib/format'
import { computeTimeRange } from '@/lib/time'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'

interface PeakTpmChartProps {
  filters: DashboardFilters & { model?: string }
}

export function PeakTpmChart(props: PeakTpmChartProps) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()

  const params = useMemo<PeakTpmParams>(() => {
    const filters = props.filters
    const range = computeTimeRange(
      getDefaultDays(filters.time_granularity),
      filters.start_timestamp,
      filters.end_timestamp
    )
    const username = filters.username?.trim()
    const model = filters.model?.trim()
    return {
      ...range,
      ...(username ? { username } : {}),
      ...(model ? { model } : {}),
    }
  }, [props.filters])

  const query = useQuery({
    queryKey: ['dashboard', 'peak-tpm', params],
    queryFn: async () => {
      const res = await getPeakTpm(params)
      if (!res.success) throw new Error(res.message || t('Request failed'))
      return res.data ?? []
    },
    retry: false,
  })

  const series = useMemo(
    () =>
      buildPeakTpmSeries(
        query.data ?? [],
        params,
        Math.floor(query.dataUpdatedAt / 1000)
      ),
    [query.data, query.dataUpdatedAt, params]
  )
  const peak = useMemo(
    () => series.reduce((max, point) => Math.max(max, point.peak_tpm), 0),
    [series]
  )
  const spec = useMemo(
    () => buildPeakTpmSpec(series, t('Peak TPM')),
    [series, t]
  )

  let body
  if (query.isPending) {
    body = <Skeleton className='h-full w-full' />
  } else if (query.isError) {
    body = (
      <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
        {t('Unable to load peak TPM')}
      </div>
    )
  } else if (series.length === 0) {
    body = (
      <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
        {t('No data available')}
      </div>
    )
  } else if (themeReady) {
    body = (
      <VChart
        key={`peak-tpm-${resolvedTheme}-${params.start_timestamp}-${params.end_timestamp}`}
        spec={{
          ...spec,
          theme: resolvedTheme === 'dark' ? 'dark' : 'light',
        }}
        option={VCHART_OPTION}
      />
    )
  }

  return (
    <section className='overflow-hidden rounded-lg border'>
      <div className='flex w-full flex-col gap-1.5 border-b px-3 py-2 sm:flex-row sm:items-center sm:justify-between sm:gap-3 sm:px-5 sm:py-3'>
        <div className='flex min-w-0 items-center gap-2'>
          <IconBadge tone='chart-2' size='sm'>
            <Activity />
          </IconBadge>
          <div className='min-w-0'>
            <h2 className='text-sm font-semibold'>{t('Hourly peak TPM')}</h2>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Busiest minute in each hour, counting prompt + completion tokens (Anthropic cache tokens excluded).'
              )}
            </p>
          </div>
        </div>
        {series.length > 0 && (
          <div className='flex shrink-0 items-baseline gap-1.5'>
            <span className='text-muted-foreground text-xs'>{t('Peak')}</span>
            <span className='font-mono text-sm font-semibold tabular-nums'>
              {formatNumber(peak)}
            </span>
          </div>
        )}
      </div>

      <div className='h-[300px] p-1.5 sm:h-96 sm:p-2'>{body}</div>
    </section>
  )
}

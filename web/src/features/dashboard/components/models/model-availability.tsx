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
import {
  Activity,
  AlertTriangle,
  HeartPulse,
  Info,
  Layers,
  Trophy,
  type LucideIcon,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  TruncatedCell,
  staticDataTableClassNames as tableStyles,
} from '@/components/data-table'
import {
  StatusBadge,
  dotColorMap,
  type StatusVariant,
} from '@/components/status-badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { IconBadge, type IconBadgeTone } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { getModelAvailability } from '@/features/dashboard/api'
import { MODEL_AVAILABILITY_WINDOW_MINUTES } from '@/features/dashboard/constants'
import { rankAvailability } from '@/features/dashboard/lib'
import type {
  ModelAvailabilityMinutes,
  ModelAvailabilityStatusId,
  RankedModelAvailabilityRow,
} from '@/features/dashboard/types'
import { toIntlLocale } from '@/i18n/languages'
import {
  formatCompactNumber,
  formatNumber,
  formatPercent,
  formatTimestampRelative,
  formatTimestampToDate,
} from '@/lib/format'
import { cn } from '@/lib/utils'

const AVAILABILITY_STALE_MS = 30 * 1000
const AVAILABILITY_REFETCH_MS = 60 * 1000

const STATUS_VARIANTS: Record<ModelAvailabilityStatusId, StatusVariant> = {
  stable: 'success',
  fair: 'info',
  degraded: 'warning',
  unstable: 'danger',
  insufficient_samples: 'neutral',
}

const SKELETON_ROW_KEYS = ['first', 'second', 'third', 'fourth'] as const

export function ModelAvailability() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [minutes, setMinutes] = useState<ModelAvailabilityMinutes>(10)

  const query = useQuery({
    queryKey: ['dashboard', 'model-availability', minutes],
    queryFn: async () => {
      const res = await getModelAvailability(minutes)
      if (!res.success || !res.data) {
        throw new Error(res.message || t('Request failed'))
      }
      return res.data
    },
    staleTime: AVAILABILITY_STALE_MS,
    refetchInterval: AVAILABILITY_REFETCH_MS,
    retry: false,
  })

  const rows = useMemo(
    () => rankAvailability(query.data?.models ?? [], minutes),
    [query.data, minutes]
  )
  const totals = useMemo(() => {
    let success = 0
    let streamErrors = 0
    let failed = 0
    let lastCalledAt = 0
    for (const row of rows) {
      success += row.success_count
      streamErrors += row.stream_error_count
      failed += row.failed_count
      lastCalledAt = Math.max(lastCalledAt, row.last_called_at)
    }
    return { success, streamErrors, failed, lastCalledAt }
  }, [rows])

  // Preserve data when a background refresh fails.
  const loading = query.isPending
  const loadFailed = query.isError && !query.data
  const topRow = rows.at(0)
  const callBreakdown = t(
    '{{success}} succeeded · {{interrupted}} interrupted · {{failed}} failed',
    {
      success: formatNumber(totals.success, locale),
      interrupted: formatNumber(totals.streamErrors, locale),
      failed: formatNumber(totals.failed, locale),
    }
  )

  let body
  if (loadFailed) {
    body = (
      <div className='text-muted-foreground px-4 py-10 text-center text-sm'>
        {t('Unable to load model availability')}
      </div>
    )
  } else {
    body = (
      <>
        {query.data?.error_log_enabled === false && (
          <div className='px-3 pt-3 sm:px-4 sm:pt-4'>
            <Alert className='border-amber-200 bg-amber-50 py-1.5 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-50'>
              <AlertTriangle aria-hidden='true' />
              <AlertDescription className='text-xs text-current'>
                {t(
                  'Error logging is disabled, so failed requests are not counted here. Enable ERROR_LOG_ENABLED for accurate availability.'
                )}
              </AlertDescription>
            </Alert>
          </div>
        )}
        <div className='grid grid-cols-1 gap-2 p-3 sm:grid-cols-3 sm:p-4'>
          <OverviewStat
            icon={Trophy}
            tone='warning'
            label={t('Top model')}
            loading={loading}
            value={topRow?.model_name ?? '-'}
            valueClassName='font-mono'
            detail={
              topRow
                ? `${formatPercent(topRow.rate)} · ${t(topRow.status.labelKey)}`
                : '-'
            }
          />
          <OverviewStat
            icon={Activity}
            tone='info'
            label={t('Requests')}
            loading={loading}
            value={formatNumber(
              totals.success + totals.streamErrors + totals.failed,
              locale
            )}
            detail={callBreakdown}
          />
          <OverviewStat
            icon={Layers}
            tone='chart-4'
            label={t('Active models')}
            loading={loading}
            value={formatNumber(rows.length, locale)}
            detail={
              totals.lastCalledAt > 0
                ? t('Last call {{time}}', {
                    time: formatTimestampRelative(
                      totals.lastCalledAt,
                      'seconds',
                      locale
                    ),
                  })
                : '-'
            }
          />
        </div>
        {loading ? (
          <div className='space-y-2 border-t p-4'>
            {SKELETON_ROW_KEYS.map((key) => (
              <Skeleton key={key} className='h-8 w-full' />
            ))}
          </div>
        ) : (
          <ModelAvailabilityTable rows={rows} locale={locale} />
        )}
      </>
    )
  }

  return (
    <section className='overflow-hidden rounded-lg border'>
      <div className='flex flex-wrap items-center justify-between gap-2 border-b px-4 py-3 sm:px-5'>
        <div className='flex min-w-0 items-center gap-2'>
          <IconBadge tone='success' size='sm'>
            <HeartPulse />
          </IconBadge>
          <div className='min-w-0'>
            <div className='flex items-center gap-1'>
              <h2 className='text-sm font-semibold'>
                {t('Model availability')}
              </h2>
              <Tooltip>
                <TooltipTrigger
                  render={
                    <button
                      type='button'
                      className='text-muted-foreground/60 hover:text-foreground flex size-5 shrink-0 items-center justify-center rounded-md'
                      aria-label={t('How availability is calculated')}
                    />
                  }
                >
                  <Info className='size-3.5' aria-hidden='true' />
                </TooltipTrigger>
                <TooltipContent className='max-w-xs flex-col items-start'>
                  <p>
                    {t(
                      'Availability is successful calls out of all calls; interrupted streams count as unsuccessful.'
                    )}
                  </p>
                  <p>
                    {t(
                      'Failures count final failures only: a request retried across channels counts once.'
                    )}
                  </p>
                  <p>
                    {t(
                      'Models are ranked by the lower bound of the 95% confidence interval, so low-traffic models rank lower.'
                    )}
                  </p>
                </TooltipContent>
              </Tooltip>
            </div>
            <p className='text-muted-foreground text-xs'>
              {t('Recent request health for models you can use')}
            </p>
          </div>
        </div>
        <div
          role='group'
          aria-label={t('Time range')}
          className='bg-muted/50 inline-flex rounded-lg border p-0.5'
        >
          {MODEL_AVAILABILITY_WINDOW_MINUTES.map((value) => (
            <button
              key={value}
              type='button'
              aria-pressed={minutes === value}
              onClick={() => setMinutes(value)}
              className={cn(
                'rounded-md px-2.5 py-1 text-xs',
                minutes === value
                  ? 'bg-background shadow-sm'
                  : 'text-muted-foreground'
              )}
            >
              {value === 60
                ? t('1 hour')
                : t('{{count}} minutes', { count: value })}
            </button>
          ))}
        </div>
      </div>
      {body}
    </section>
  )
}

function OverviewStat(props: {
  icon: LucideIcon
  tone: IconBadgeTone
  label: string
  loading: boolean
  value: string
  valueClassName?: string
  detail: string
}) {
  const Icon = props.icon
  return (
    <div className='bg-muted/40 min-w-0 rounded-lg px-3 py-2.5'>
      <div className='text-muted-foreground flex items-center gap-1.5 text-[11px] font-medium'>
        <IconBadge tone={props.tone} size='xs'>
          <Icon />
        </IconBadge>
        <span className='truncate'>{props.label}</span>
      </div>
      {props.loading ? (
        <>
          <Skeleton className='mt-1.5 h-5 w-24' />
          <Skeleton className='mt-1 h-3 w-32' />
        </>
      ) : (
        <>
          <div
            className={cn(
              'mt-1.5 truncate text-sm font-semibold tabular-nums',
              props.valueClassName
            )}
            title={props.value}
          >
            {props.value}
          </div>
          <div
            className='text-muted-foreground mt-0.5 truncate text-[11px]'
            title={props.detail}
          >
            {props.detail}
          </div>
        </>
      )}
    </div>
  )
}

function ModelAvailabilityTable(props: {
  rows: RankedModelAvailabilityRow[]
  locale: string | undefined
}) {
  const { t } = useTranslation()
  const locale = props.locale

  return (
    <div className='max-h-[28rem] overflow-auto border-t'>
      <StaticDataTable
        className='rounded-none border-0'
        tableClassName='min-w-[960px] text-sm [&_tbody>tr]:h-10'
        headerRowClassName={cn(
          tableStyles.mutedHeaderRow,
          'sticky top-0 z-10 bg-background'
        )}
        data={props.rows}
        getRowKey={(row) => row.model_name}
        emptyContent={t('No model calls in this window')}
        columns={[
          {
            id: 'rank',
            header: t('Rank'),
            className: cn(tableStyles.compactHeaderCell, 'w-12'),
            cellClassName: 'text-muted-foreground py-1.5 font-mono',
            cell: (row) => row.rank,
          },
          {
            id: 'model',
            header: t('Model'),
            className: tableStyles.compactHeaderCell,
            cellClassName: 'py-1.5',
            cell: (row) => (
              <span className='font-mono text-sm'>
                {row.model_name || t('Unknown model')}
              </span>
            ),
          },
          {
            id: 'status',
            header: t('Status'),
            className: tableStyles.compactHeaderCell,
            cellClassName: 'py-1.5',
            cell: (row) => (
              <StatusBadge
                label={t(row.status.labelKey)}
                variant={STATUS_VARIANTS[row.status.id]}
                copyable={false}
              />
            ),
          },
          {
            id: 'availability',
            header: t('Availability'),
            className: tableStyles.compactHeaderCellRight,
            cellClassName: 'py-1.5',
            cell: (row) => (
              <div className='flex items-center justify-end gap-2'>
                <div
                  className='bg-muted h-1.5 w-16 overflow-hidden rounded-full'
                  aria-hidden='true'
                >
                  <div
                    className={cn(
                      'h-full rounded-full',
                      dotColorMap[STATUS_VARIANTS[row.status.id]]
                    )}
                    style={{ width: `${row.rate ?? 0}%` }}
                  />
                </div>
                <span className='w-16 text-right font-mono tabular-nums'>
                  {formatPercent(row.rate)}
                </span>
              </div>
            ),
          },
          {
            id: 'requests',
            header: t('Requests'),
            className: tableStyles.compactHeaderCellRight,
            cellClassName: 'py-1.5 text-right font-mono',
            cell: (row) => (
              <TruncatedCell
                tooltipContent={t(
                  '{{success}} succeeded · {{interrupted}} interrupted · {{failed}} failed',
                  {
                    success: formatNumber(row.success_count, locale),
                    interrupted: formatNumber(row.stream_error_count, locale),
                    failed: formatNumber(row.failed_count, locale),
                  }
                )}
              >
                <span>{formatCompactNumber(row.total, locale)}</span>
              </TruncatedCell>
            ),
          },
          {
            id: 'tokens',
            header: t('Tokens'),
            className: tableStyles.compactHeaderCellRight,
            cellClassName: 'py-1.5 text-right font-mono',
            cell: (row) => formatCompactNumber(row.total_tokens, locale),
          },
          {
            id: 'average-rpm',
            header: t('Average RPM'),
            className: tableStyles.compactHeaderCellRight,
            cellClassName: 'py-1.5 text-right font-mono',
            cell: (row) => formatNumber(row.rpm, locale),
          },
          {
            id: 'average-tpm',
            header: t('Average TPM'),
            className: tableStyles.compactHeaderCellRight,
            cellClassName: 'py-1.5 text-right font-mono',
            cell: (row) => formatCompactNumber(row.tpm, locale),
          },
          {
            id: 'share',
            header: t('Share'),
            className: tableStyles.compactHeaderCellRight,
            cellClassName: 'py-1.5 text-right font-mono',
            cell: (row) => formatPercent(row.share),
          },
          {
            id: 'last-called',
            header: t('Last called'),
            className: tableStyles.compactHeaderCellRight,
            cellClassName: 'text-muted-foreground py-1.5 text-right',
            cell: (row) => (
              <TruncatedCell
                tooltipContent={formatTimestampToDate(row.last_called_at)}
              >
                <span>
                  {formatTimestampRelative(
                    row.last_called_at,
                    'seconds',
                    locale
                  )}
                </span>
              </TruncatedCell>
            ),
          },
        ]}
      />
    </div>
  )
}

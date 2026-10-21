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
import type { ColumnDef } from '@tanstack/react-table'
import {
  ChevronDown,
  ChevronRight,
  Loader2,
  TableProperties,
} from 'lucide-react'
import { Fragment, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTablePagination,
  DataTableRow,
  DataTableView,
  DataTableViewOptions,
  StaticDataTable,
  staticDataTableClassNames as tableStyles,
  useDataTable,
} from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { IconBadge } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { TableCell, TableRow } from '@/components/ui/table'
import { getUsageAnalytics } from '@/features/dashboard/api'
import { MetricsHint } from '@/features/dashboard/components/models/model-usage-expansion'
import {
  avgUseTime,
  cacheHitRate,
  ratePerMinute,
  successRate,
} from '@/features/dashboard/lib'
import type {
  UsageAnalyticsRow,
  UsageMetrics,
} from '@/features/dashboard/types'
import {
  formatCompactNumber,
  formatNumber,
  formatPercent,
  formatQuota,
  formatTimestampToDate,
  formatUseTime,
} from '@/lib/format'
import { getRollingDateRange } from '@/lib/time'
import { cn } from '@/lib/utils'

const COLUMN_VISIBILITY_STORAGE_KEY = 'dashboard-user-usage:column-visibility'
const NO_ROWS: UsageAnalyticsRow[] = []
const INITIAL_SORTING = [{ id: 'quota', desc: true }]
const INITIAL_PAGINATION = { pageIndex: 0, pageSize: 20 }
const MODEL_SKELETON_ROWS = ['first', 'second', 'third'] as const

interface UsageTimeRange {
  start_timestamp: number
  end_timestamp: number
}

interface UserUsageTableProps {
  // Rolling range in days.
  selectedRange: number
}

function SuccessRateCell(props: {
  metrics: Pick<
    UsageMetrics,
    'tracked_count' | 'stream_error_count' | 'failed_count'
  >
}) {
  return (
    <span className='font-mono'>
      {formatPercent(successRate(props.metrics))}
      <span
        className={cn(
          'ml-1.5',
          props.metrics.failed_count > 0
            ? 'text-destructive'
            : 'text-muted-foreground'
        )}
      >
        / {formatNumber(props.metrics.failed_count)}
      </span>
    </span>
  )
}

function UserModelUsage(props: {
  username: string
  timeRange: UsageTimeRange
}) {
  const { t } = useTranslation()
  const { data, isLoading, isError } = useQuery({
    queryKey: [
      'dashboard',
      'user-usage-table',
      'user_model',
      props.username,
      props.timeRange,
    ],
    queryFn: async () => {
      const res = await getUsageAnalytics(
        {
          ...props.timeRange,
          group_by: 'user_model',
          username: props.username,
        },
        true
      )
      if (!res.success) throw new Error(res.message)
      return res.data?.rows ?? []
    },
    staleTime: 60_000,
    retry: false,
  })

  if (isLoading) {
    return (
      <div className='space-y-2 py-1'>
        {MODEL_SKELETON_ROWS.map((key) => (
          <Skeleton key={key} className='h-6 w-full' />
        ))}
      </div>
    )
  }

  const rows = data ?? []
  const numericCell = 'py-1.5 font-mono'

  return (
    <StaticDataTable
      className='bg-background'
      tableClassName='min-w-[1100px] text-sm [&_tbody>tr]:h-9'
      headerRowClassName={tableStyles.mutedHeaderRow}
      data={rows}
      getRowKey={(row) =>
        `${row.user_id ?? 0}:${row.username ?? ''}:${row.model_name ?? ''}`
      }
      emptyContent={isError ? t('Failed to load') : t('No data available')}
      emptyClassName='text-muted-foreground h-16'
      columns={[
        {
          id: 'model',
          header: t('Model'),
          className: tableStyles.compactHeaderCell,
          cellClassName: 'py-1.5 font-mono',
          cell: (row) => row.model_name || t('Unknown model'),
        },
        {
          id: 'requests',
          header: t('Requests'),
          className: tableStyles.compactHeaderCell,
          cellClassName: numericCell,
          cell: (row) => formatCompactNumber(row.request_count),
        },
        {
          id: 'quota',
          header: t('Quota'),
          className: tableStyles.compactHeaderCell,
          cellClassName: numericCell,
          cell: (row) => formatQuota(row.quota),
        },
        {
          id: 'tokens',
          header: t('Tokens'),
          className: tableStyles.compactHeaderCell,
          cellClassName: numericCell,
          cell: (row) => formatCompactNumber(row.token_used),
        },
        {
          id: 'input',
          header: t('Input'),
          className: tableStyles.compactHeaderCell,
          cellClassName: numericCell,
          cell: (row) => formatCompactNumber(row.input_tokens),
        },
        {
          id: 'output',
          header: t('Output'),
          className: tableStyles.compactHeaderCell,
          cellClassName: numericCell,
          cell: (row) => formatCompactNumber(row.completion_tokens),
        },
        {
          id: 'cache-write',
          header: t('Cache Write'),
          className: tableStyles.compactHeaderCell,
          cellClassName: numericCell,
          cell: (row) => formatCompactNumber(row.cache_write_tokens),
        },
        {
          id: 'cache-read',
          header: t('Cache Read'),
          className: tableStyles.compactHeaderCell,
          cellClassName: numericCell,
          cell: (row) => formatCompactNumber(row.cache_read_tokens),
        },
        {
          id: 'cache-hit-rate',
          header: t('Cache hit rate'),
          className: tableStyles.compactHeaderCell,
          cellClassName: numericCell,
          cell: (row) => formatPercent(cacheHitRate(row)),
        },
        {
          id: 'latency',
          header: t('Average latency'),
          className: tableStyles.compactHeaderCell,
          cellClassName: numericCell,
          cell: (row) => {
            const seconds = avgUseTime(row)
            return seconds === null ? '-' : formatUseTime(seconds)
          },
        },
        {
          id: 'success-rate',
          header: t('Success rate / Failures'),
          className: tableStyles.compactHeaderCell,
          cellClassName: 'py-1.5',
          cell: (row) => <SuccessRateCell metrics={row} />,
        },
        {
          id: 'last-called',
          header: t('Last called'),
          className: tableStyles.compactHeaderCell,
          cellClassName: 'text-muted-foreground py-1.5 font-mono',
          cell: (row) => formatTimestampToDate(row.last_called_at),
        },
      ]}
    />
  )
}

// Keep this reference stable for memoized table rows.
function getUserUsageColumnClassName(
  columnId: string,
  kind: 'header' | 'cell'
) {
  if (kind === 'header' || columnId === 'expand' || columnId === 'user') {
    return undefined
  }
  return 'font-mono'
}

function useUserUsageColumns(
  minutes: number
): ColumnDef<UsageAnalyticsRow, unknown>[] {
  const { t } = useTranslation()

  return useMemo(
    () => [
      {
        id: 'expand',
        header: () => <span className='sr-only'>{t('Expand')}</span>,
        cell: ({ row }) => {
          const expanded = row.getIsExpanded()
          return (
            <Button
              variant='ghost'
              size='icon-xs'
              aria-expanded={expanded}
              aria-label={expanded ? t('Collapse') : t('Expand')}
              disabled={!row.original.username}
              onClick={() => row.toggleExpanded()}
            >
              {expanded ? (
                <ChevronDown aria-hidden='true' />
              ) : (
                <ChevronRight aria-hidden='true' />
              )}
            </Button>
          )
        },
        enableSorting: false,
        enableHiding: false,
      },
      {
        id: 'user',
        accessorFn: (row) => row.username ?? '',
        header: t('User'),
        cell: ({ row }) => {
          const username = row.original.username || `#${row.original.user_id}`
          const displayName = row.original.display_name
          return (
            <div className='flex min-w-[140px] flex-col'>
              <span className='truncate font-medium'>{username}</span>
              {displayName && displayName !== username && (
                <span className='text-muted-foreground truncate text-xs'>
                  {displayName}
                </span>
              )}
            </div>
          )
        },
        enableHiding: false,
      },
      {
        id: 'requests',
        accessorKey: 'request_count',
        header: t('Requests'),
        cell: ({ row }) => formatCompactNumber(row.original.request_count),
      },
      {
        id: 'quota',
        accessorKey: 'quota',
        header: t('Quota'),
        cell: ({ row }) => formatQuota(row.original.quota),
      },
      {
        id: 'tokens',
        accessorKey: 'token_used',
        header: t('Tokens'),
        cell: ({ row }) => formatCompactNumber(row.original.token_used),
      },
      {
        id: 'average-rpm',
        accessorFn: (row) => ratePerMinute(row.request_count, minutes),
        header: t('Average RPM'),
        cell: ({ getValue }) => formatNumber(getValue<number>()),
      },
      {
        id: 'average-tpm',
        accessorFn: (row) => ratePerMinute(row.token_used, minutes),
        header: t('Average TPM'),
        cell: ({ getValue }) => formatCompactNumber(getValue<number>()),
      },
      {
        id: 'success-rate',
        // Untracked (legacy-only) users sort last in both directions.
        accessorFn: (row) => successRate(row) ?? undefined,
        sortUndefined: 'last',
        header: t('Success rate / Failures'),
        cell: ({ row }) => <SuccessRateCell metrics={row.original} />,
      },
      {
        id: 'last-called',
        accessorKey: 'last_called_at',
        header: t('Last called'),
        cell: ({ row }) => formatTimestampToDate(row.original.last_called_at),
      },
    ],
    [minutes, t]
  )
}

export function UserUsageTable(props: UserUsageTableProps) {
  const { t } = useTranslation()

  const timeRange = useMemo<UsageTimeRange>(() => {
    const { start, end } = getRollingDateRange(props.selectedRange)
    return {
      start_timestamp: Math.floor(start.getTime() / 1000),
      end_timestamp: Math.floor(end.getTime() / 1000),
    }
  }, [props.selectedRange])
  const minutes = (timeRange.end_timestamp - timeRange.start_timestamp) / 60

  const { data, isLoading, isFetching, isError } = useQuery({
    queryKey: ['dashboard', 'user-usage-table', 'user', timeRange],
    queryFn: async () => {
      const res = await getUsageAnalytics(
        { ...timeRange, group_by: 'user' },
        true
      )
      if (!res.success) throw new Error(res.message)
      return res.data?.rows ?? []
    },
    staleTime: 60_000,
    retry: false,
  })

  const columns = useUserUsageColumns(minutes)
  const { table } = useDataTable({
    data: data ?? NO_ROWS,
    columns,
    // Renamed users can have multiple rows with the same user_id.
    getRowId: (row) => `${row.user_id ?? 0}:${row.username ?? ''}`,
    initialSorting: INITIAL_SORTING,
    initialPagination: INITIAL_PAGINATION,
    columnVisibilityStorageKey: COLUMN_VISIBILITY_STORAGE_KEY,
  })

  return (
    <section className='overflow-hidden rounded-lg border'>
      <div className='flex flex-wrap items-center gap-2 border-b px-3 py-2 sm:px-5 sm:py-3'>
        <IconBadge tone='info' size='sm'>
          <TableProperties />
        </IconBadge>
        <h2 className='text-sm font-semibold'>{t('User usage details')}</h2>
        <MetricsHint
          lines={[
            t(
              'Failures count final failures only: a request retried across channels counts once.'
            ),
            t(
              'Success rate, cache hit rate and latency only cover requests recorded after this release.'
            ),
          ]}
        />
        {isFetching && !isLoading && (
          <Loader2
            className='text-muted-foreground size-4 animate-spin'
            aria-hidden='true'
          />
        )}
        <div className='ml-auto'>
          <DataTableViewOptions table={table} />
        </div>
      </div>

      <DataTableView
        table={table}
        isLoading={isLoading}
        skeletonKeyPrefix='user-usage-skeleton'
        containerClassName='rounded-none border-0'
        tableClassName='min-w-[860px]'
        tableHeaderClassName='[background-color:var(--table-header)]'
        getColumnClassName={getUserUsageColumnClassName}
        emptyContent={isError ? t('Failed to load') : t('No data available')}
        emptyCellClassName='text-muted-foreground h-24 text-center'
        renderRow={(row) => {
          const expanded = row.getIsExpanded()
          return (
            <Fragment key={row.id}>
              <DataTableRow
                row={row}
                className={expanded ? 'border-b-0' : undefined}
                getColumnClassName={getUserUsageColumnClassName}
                cellRenderColumns={table.options.columns}
              />
              {expanded && row.original.username && (
                <TableRow className='hover:bg-transparent'>
                  <TableCell
                    colSpan={row.getVisibleCells().length}
                    className='bg-muted/30 px-3 pt-0 pb-3 sm:px-5'
                  >
                    {/* Constrain the breakdown so it scrolls inside the cell. */}
                    <div className='w-0 min-w-full'>
                      <UserModelUsage
                        username={row.original.username}
                        timeRange={timeRange}
                      />
                    </div>
                  </TableCell>
                </TableRow>
              )}
            </Fragment>
          )
        }}
      />

      {table.getRowModel().rows.length > 0 && (
        <div className='border-t px-3 py-2 sm:px-5'>
          <DataTablePagination table={table} />
        </div>
      )}
    </section>
  )
}

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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, ChevronRight, ExternalLink, Pencil, Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { getLobeIcon } from '@/lib/lobe-icon'
import { formatBillingCurrencyFromUSD } from '@/lib/currency'
import { invalidateModelSquareQueries } from '@/features/pricing/lib/query-keys'

import {
  deleteProvider,
  getAllProviderModels,
  getProviderModels,
  getProviders,
} from '../api'
import { providerModelQueryKeys, providersQueryKeys } from '../lib'
import type { Provider, ProviderModelRelation } from '../types'
import { ModelProviderConfigDrawer } from './drawers/model-provider-config-drawer'
import { ProviderModelAddDrawer } from './drawers/provider-model-add-drawer'
import { useModels } from './models-provider'

const EMPTY_PROVIDER_MODEL_RELATIONS: ProviderModelRelation[] = []

function ProviderPriceValue(props: {
  price: number | null | undefined
  listPrice?: number | null
}) {
  const hasListPrice =
    props.listPrice != null &&
    Number.isFinite(props.listPrice) &&
    props.listPrice > (props.price ?? Number.POSITIVE_INFINITY)
  if (!hasListPrice) {
    return props.price == null ? '—' : formatBillingCurrencyFromUSD(props.price)
  }
  return (
    <span className='inline-flex flex-wrap items-baseline justify-end gap-1'>
      <span className='text-muted-foreground/70 text-[11px] line-through'>
        {formatBillingCurrencyFromUSD(props.listPrice)}
      </span>
      <span className='text-emerald-600 dark:text-emerald-400'>
        {formatBillingCurrencyFromUSD(props.price)}
      </span>
    </span>
  )
}

function ProviderDiscountValue(props: { price?: ProviderModelRelation['price'] }) {
  const { t } = useTranslation()
  const rate = props.price?.discount_rate
  if (
    rate == null ||
    !Number.isFinite(rate) ||
    rate < 0 ||
    rate >= 100
  ) {
    return <span className='text-muted-foreground'>—</span>
  }
  const hasListPrice = [
    [props.price?.input_list_price, props.price?.input_price],
    [props.price?.output_list_price, props.price?.output_price],
    [props.price?.cache_read_list_price, props.price?.cache_read_price],
    [props.price?.cache_write_list_price, props.price?.cache_write_price],
  ].some(([listPrice, price]) => listPrice != null && price != null && listPrice > price)
  if (!hasListPrice) return <span className='text-muted-foreground'>—</span>
  return (
    <span className='bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 rounded px-1.5 py-0.5 text-xs font-medium'>
      {t('{{rate}} fold', {
        rate: (rate / 10).toLocaleString(undefined, { maximumFractionDigits: 2 }),
      })}
    </span>
  )
}

function ProviderModelsPanel(props: { provider: Provider }) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [selectedModel, setSelectedModel] =
    useState<ProviderModelRelation | null>(null)
  const [addModelOpen, setAddModelOpen] = useState(false)
  const [configOpen, setConfigOpen] = useState(false)
  const pageSize = 24
  const query = useQuery({
    queryKey: providerModelQueryKeys.list(props.provider.slug, {
      p: page,
      page_size: pageSize,
    }),
    queryFn: () =>
      getProviderModels(props.provider.slug, {
        p: page,
        page_size: pageSize,
      }),
    staleTime: 0,
    refetchOnMount: 'always',
  })
  const configuredModelsQuery = useQuery({
    queryKey: providerModelQueryKeys.list(props.provider.slug, { all: true }),
    queryFn: () => getAllProviderModels(props.provider.slug),
    enabled: addModelOpen,
    staleTime: 0,
    refetchOnMount: 'always',
  })
  const items =
    query.data?.data?.items ?? EMPTY_PROVIDER_MODEL_RELATIONS
  const total = query.data?.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const configuredModelIds = useMemo(
    () =>
      new Set(
        (configuredModelsQuery.data ?? items).map((item) => item.model_id)
      ),
    [configuredModelsQuery.data, items]
  )

  let modelsContent: React.ReactNode
  if (query.isLoading) {
    modelsContent = (
      <div className='text-muted-foreground py-4 text-center text-sm'>
        {t('Loading...')}
      </div>
    )
  } else if (items.length === 0) {
    modelsContent = (
      <div className='text-muted-foreground rounded-md border border-dashed px-3 py-4 text-center text-sm'>
        {t('No models configured. Use Add model to get started.')}
      </div>
    )
  } else {
    modelsContent = (
      <div className='overflow-x-auto rounded-md border'>
        <table className='w-max min-w-[920px] table-fixed text-xs'>
          <colgroup>
            <col className='w-[180px]' />
            <col className='w-[96px]' />
            <col className='w-[180px]' />
            <col className='w-[120px]' />
            <col className='w-[120px]' />
            <col className='w-[100px]' />
            <col className='w-[70px]' />
            <col className='w-[96px]' />
          </colgroup>
          <thead className='bg-background text-muted-foreground text-xs'>
            <tr>
              <th className='w-[180px] px-2 py-1.5 text-left font-medium'>{t('Model')}</th>
              <th className='w-[96px] px-2 py-1.5 text-left font-medium'>{t('Status')}</th>
              <th className='w-[180px] px-2 py-1.5 text-left font-medium'>{t('Provider model name')}</th>
              <th className='w-[120px] px-2 py-1.5 text-right font-medium'>{t('Input')}</th>
              <th className='w-[120px] px-2 py-1.5 text-right font-medium'>{t('Output')}</th>
              <th className='w-[100px] px-2 py-1.5 text-right font-medium'>{t('Cache')}</th>
              <th className='w-[70px] px-2 py-1.5 text-right font-medium'>{t('Discount')}</th>
              <th className='w-[96px] px-2 py-1.5 text-right font-medium'>{t('Actions')}</th>
            </tr>
          </thead>
          <tbody className='divide-y'>
            {items.map((item) => (
                <ProviderModelRow
                  key={item.model_id}
                  item={item}
                  onConfigure={() => {
                    setSelectedModel(item)
                    setConfigOpen(true)
                  }}
                />
            ))}
          </tbody>
        </table>
      </div>
    )
  }

  return (
    <div className='bg-muted/20 space-y-2 p-2 sm:p-3'>
      <div className='flex items-center justify-between gap-2'>
        <div className='min-w-0'>
          <div className='text-sm font-semibold'>{t('Provider models')}</div>
          <div className='text-muted-foreground text-xs'>
            {t('{{count}} models', { count: total })}
          </div>
        </div>
        <div className='flex items-center gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            className='h-7 gap-1 px-2 text-xs'
            disabled={query.isLoading || query.isFetching}
            onClick={() => setAddModelOpen(true)}
          >
            <Plus className='size-3.5' />
            {t('Add model')}
          </Button>
          {props.provider.website_url && (
            <a
              href={props.provider.website_url}
              target='_blank'
              rel='noreferrer'
              className='text-primary inline-flex items-center gap-1 text-xs hover:underline'
            >
              {t('Website')}
              <ExternalLink className='size-3' />
            </a>
          )}
        </div>
      </div>
      {modelsContent}
      {totalPages > 1 && (
        <div className='flex items-center justify-end gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={page <= 1}
            onClick={() => setPage((value) => value - 1)}
          >
            {t('Previous page')}
          </Button>
          <span className='text-muted-foreground text-xs'>
            {page} / {totalPages}
          </span>
          <Button
            variant='outline'
            size='sm'
            disabled={page >= totalPages}
            onClick={() => setPage((value) => value + 1)}
          >
            {t('Next page')}
          </Button>
        </div>
      )}
      <ModelProviderConfigDrawer
        open={configOpen}
        onOpenChange={(open) => {
          setConfigOpen(open)
          if (!open) setSelectedModel(null)
        }}
        modelId={selectedModel?.model_id}
        modelName={selectedModel?.model_name}
        providerSlug={props.provider.slug}
        providerName={props.provider.display_name}
      />
      <ProviderModelAddDrawer
        open={addModelOpen}
        onOpenChange={setAddModelOpen}
        provider={props.provider}
        configuredModelIds={configuredModelIds}
        configuredModelsLoading={
          configuredModelsQuery.isLoading || configuredModelsQuery.isFetching
        }
        configuredModelsError={configuredModelsQuery.isError}
        onSelect={(model) => {
          setAddModelOpen(false)
          setSelectedModel({
            model_id: model.id,
            model_name: model.model_name,
            model_icon: model.icon,
            configured: false,
            available: false,
          })
          setConfigOpen(true)
        }}
      />
    </div>
  )
}

function ProviderModelRow(props: {
  item: ProviderModelRelation
  onConfigure: () => void
}) {
  const { t } = useTranslation()
  const modelIconKey =
    props.item.model_icon || props.item.vendor_icon || undefined
  const modelIcon = modelIconKey ? getLobeIcon(modelIconKey, 18) : null
  return (
    <tr>
      <td className='px-2 py-1.5'>
        <div className='flex items-center gap-2'>
          <div className='bg-muted flex size-6 shrink-0 items-center justify-center rounded-md'>
            {modelIcon || (
              <span className='text-muted-foreground text-xs font-semibold'>
                {props.item.model_name.charAt(0).toUpperCase()}
              </span>
            )}
          </div>
          <div className='min-w-0'>
            <span className='block font-mono break-all'>{props.item.model_name}</span>
            {props.item.vendor_name && (
              <span className='text-muted-foreground block text-xs'>
                {props.item.vendor_name}
              </span>
            )}
          </div>
        </div>
      </td>
      <td className='px-2 py-1.5'>
        <div className='flex flex-wrap gap-0.5'>
          <span className='bg-muted rounded px-1 py-0.5 text-[11px]'>
            {props.item.available ? t('Available') : t('Unavailable')}
          </span>
          <span className='bg-muted rounded px-1 py-0.5 text-[11px]'>
            {props.item.configured ? t('Configured') : t('Not configured')}
          </span>
        </div>
      </td>
      <td className='max-w-44 px-2 py-1.5 font-mono text-[11px] leading-snug break-all whitespace-normal'>
        {props.item.price?.model_name || '—'}
      </td>
      <td className='px-2 py-1.5 text-right font-mono tabular-nums'>
        <ProviderPriceValue
          price={props.item.price?.input_price}
          listPrice={props.item.price?.input_list_price}
        />
      </td>
      <td className='px-2 py-1.5 text-right font-mono tabular-nums'>
        <ProviderPriceValue
          price={props.item.price?.output_price}
          listPrice={props.item.price?.output_list_price}
        />
      </td>
      <td className='px-2 py-1.5 text-right font-mono tabular-nums'>
        <ProviderPriceValue
          price={props.item.price?.cache_read_price}
          listPrice={props.item.price?.cache_read_list_price}
        />
      </td>
      <td className='px-2 py-1.5 text-right'>
        <ProviderDiscountValue price={props.item.price} />
      </td>
      <td className='px-2 py-1.5 text-right'>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          className='h-7 gap-1 px-1.5 text-[11px]'
          onClick={props.onConfigure}
        >
          <Pencil className='size-3.5' />
          {props.item.configured ? t('Edit') : t('Configure')}
        </Button>
      </td>
    </tr>
  )
}

function ProviderRow(props: {
  provider: Provider
  expanded: boolean
  onToggle: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  return (
    <>
      <tr>
        <td className='px-3 py-2'>
          <div className='flex items-center gap-2'>
            <Button
              variant='ghost'
              size='icon'
              className='size-7'
              aria-label={t('Expand provider models')}
              aria-expanded={props.expanded}
              onClick={props.onToggle}
            >
              {props.expanded ? (
                <ChevronDown className='size-4' />
              ) : (
                <ChevronRight className='size-4' />
              )}
            </Button>
            <div className='flex items-center gap-2'>
              <div className='bg-muted flex size-7 shrink-0 items-center justify-center rounded-md'>
                {props.provider.icon ? (
                  getLobeIcon(props.provider.icon, 18)
                ) : (
                  <span className='text-muted-foreground text-xs font-semibold'>
                    {props.provider.display_name.charAt(0).toUpperCase()}
                  </span>
                )}
              </div>
              <span className='font-medium'>{props.provider.display_name}</span>
            </div>
          </div>
        </td>
        <td className='text-muted-foreground px-3 py-2 font-mono'>
          {props.provider.slug}
        </td>
        <td className='px-3 py-2'>
          {props.provider.status === 1 ? t('Enabled') : t('Disabled')}
        </td>
        <td className='px-3 py-2 text-right'>
          <div className='inline-flex gap-1'>
            <Button
              variant='ghost'
              size='icon'
              aria-label={t('Edit')}
              onClick={props.onEdit}
            >
              <Pencil className='size-4' />
            </Button>
            <Button
              variant='ghost'
              size='icon'
              aria-label={t('Delete')}
              onClick={props.onDelete}
            >
              <Trash2 className='size-4' />
            </Button>
          </div>
        </td>
      </tr>
      {props.expanded && (
        <tr>
          <td colSpan={4} className='p-0'>
            <ProviderModelsPanel provider={props.provider} />
          </td>
        </tr>
      )}
    </>
  )
}

export function ProvidersTable() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { setOpen, setCurrentProvider } = useModels()
  const [page, setPage] = useState(1)
  const [expandedIds, setExpandedIds] = useState<Set<number>>(new Set())
  const pageSize = 20
  const query = useQuery({
    queryKey: providersQueryKeys.list({ p: page, page_size: pageSize }),
    queryFn: () => getProviders({ p: page, page_size: pageSize }),
  })
  const providers = query.data?.data?.items ?? []
  const total = query.data?.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const handleDelete = async (id: number) => {
    if (!window.confirm(t('Delete this provider?'))) return
    try {
      const response = await deleteProvider(id)
      if (!response.success) throw new Error(response.message || t('Operation failed'))
      toast.success(t('Provider deleted successfully'))
      queryClient.invalidateQueries({ queryKey: providersQueryKeys.lists() })
      await invalidateModelSquareQueries(queryClient)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Operation failed'))
    }
  }

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between gap-3'>
        <p className='text-muted-foreground text-sm'>
          {t('Provider metadata is used by the model square.')}
        </p>
        <Button
          size='sm'
          onClick={() => {
            setCurrentProvider(null)
            setOpen('create-provider')
          }}
        >
          <Plus className='size-4' />
          {t('Add Provider')}
        </Button>
      </div>
      <div className='overflow-x-auto rounded-lg border'>
        <table className='w-full min-w-[720px] text-sm'>
          <thead className='bg-muted/40 text-muted-foreground text-xs'>
            <tr>
              <th className='px-3 py-2 text-left font-medium'>{t('Provider')}</th>
              <th className='px-3 py-2 text-left font-medium'>{t('Provider slug')}</th>
              <th className='px-3 py-2 text-left font-medium'>{t('Status')}</th>
              <th className='px-3 py-2 text-right font-medium'>{t('Actions')}</th>
            </tr>
          </thead>
          <tbody className='divide-y'>
            {providers.map((provider) => (
              <ProviderRow
                key={provider.id}
                provider={provider}
                expanded={expandedIds.has(provider.id)}
                onToggle={() =>
                  setExpandedIds((current) => {
                    const next = new Set(current)
                    if (next.has(provider.id)) next.delete(provider.id)
                    else next.add(provider.id)
                    return next
                  })
                }
                onEdit={() => {
                  setCurrentProvider(provider)
                  setOpen('update-provider')
                }}
                onDelete={() => void handleDelete(provider.id)}
              />
            ))}
          </tbody>
        </table>
      </div>
      {totalPages > 1 && (
        <div className='flex items-center justify-end gap-2'>
          <Button variant='outline' size='sm' disabled={page <= 1} onClick={() => setPage((value) => value - 1)}>
            {t('Previous page')}
          </Button>
          <span className='text-muted-foreground text-xs'>{page} / {totalPages}</span>
          <Button variant='outline' size='sm' disabled={page >= totalPages} onClick={() => setPage((value) => value + 1)}>
            {t('Next page')}
          </Button>
        </div>
      )}
    </div>
  )
}
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

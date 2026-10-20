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
import { Pencil, Plus } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  sideDrawerContentClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { formatBillingCurrencyFromUSD } from '@/lib/currency'
import { getLobeIcon } from '@/lib/lobe-icon'

import { getAllProviders, getModelProviderPrices } from '../api'
import { modelProviderPriceQueryKeys, providersQueryKeys } from '../lib'
import type { Model, ModelProviderPrice, Provider } from '../types'
import { ModelProviderConfigDrawer } from './drawers/model-provider-config-drawer'

const EMPTY_PROVIDERS: Provider[] = []
const EMPTY_PRICES: ModelProviderPrice[] = []

function ProviderPriceCell(props: {
  price?: ModelProviderPrice
  field: 'input_price' | 'output_price' | 'cache_read_price'
  listField?:
    | 'input_list_price'
    | 'output_list_price'
    | 'cache_read_list_price'
}) {
  const value = props.price?.[props.field]
  const listPrice = props.listField ? props.price?.[props.listField] : null
  const hasListPrice =
    listPrice != null &&
    Number.isFinite(listPrice) &&
    value != null &&
    Number.isFinite(value) &&
    listPrice > value
  if (value == null) return <span className='text-muted-foreground'>—</span>
  if (!hasListPrice) {
    return <span className='font-mono'>{formatBillingCurrencyFromUSD(value)}</span>
  }
  return (
    <span className='inline-flex flex-wrap items-baseline justify-end gap-1 font-mono tabular-nums'>
      <span className='text-muted-foreground/70 text-[11px] line-through'>
        {formatBillingCurrencyFromUSD(listPrice)}
      </span>
      <span className='text-emerald-600 dark:text-emerald-400'>
        {formatBillingCurrencyFromUSD(value)}
      </span>
    </span>
  )
}

function ProviderDiscountCell(props: { price?: ModelProviderPrice }) {
  const { t } = useTranslation()
  const price = props.price
  const rate = price?.discount_rate
  if (
    rate == null ||
    !Number.isFinite(rate) ||
    rate < 0 ||
    rate >= 100
  ) {
    return <span className='text-muted-foreground'>—</span>
  }
  const hasListPrice = [
    [price?.input_list_price, price?.input_price],
    [price?.output_list_price, price?.output_price],
    [price?.cache_read_list_price, price?.cache_read_price],
    [price?.cache_write_list_price, price?.cache_write_price],
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

function ProviderConfigAddDrawer(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  providers: Provider[]
  onSelect: (provider: Provider) => void
}) {
  const { t } = useTranslation()
  const [searchValue, setSearchValue] = useState('')
  const keyword = searchValue.trim().toLowerCase()
  const candidates = useMemo(
    () =>
      props.providers.filter((provider) => {
        if (!keyword) return true
        return (
          provider.display_name.toLowerCase().includes(keyword) ||
          provider.slug.toLowerCase().includes(keyword)
        )
      }),
    [keyword, props.providers]
  )

  const handleOpenChange = (open: boolean) => {
    if (!open) setSearchValue('')
    props.onOpenChange(open)
  }

  return (
    <Sheet open={props.open} onOpenChange={handleOpenChange}>
      <SheetContent
        side='right'
        className={sideDrawerContentClassName('sm:max-w-xl')}
      >
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>{t('Add provider configuration')}</SheetTitle>
          <SheetDescription>{t('Select provider')}</SheetDescription>
        </SheetHeader>
        <div className='flex min-h-0 flex-1 flex-col gap-3 px-4 pb-6 sm:px-6'>
          <div className='min-h-0 flex-1 overflow-hidden rounded-lg border'>
            <Command shouldFilter={false} className='rounded-none'>
              <CommandInput
                placeholder={t('Search')}
                value={searchValue}
                onValueChange={setSearchValue}
              />
              <CommandList className='max-h-full'>
                <CommandEmpty>{t('No results found')}</CommandEmpty>
                <CommandGroup>
                  {candidates.map((provider) => (
                    <CommandItem
                      key={provider.slug}
                      value={provider.slug}
                      onSelect={() => {
                        props.onSelect(provider)
                        handleOpenChange(false)
                      }}
                      className='items-center gap-3 rounded-lg px-3 py-2.5'
                    >
                      <div className='bg-muted flex size-8 shrink-0 items-center justify-center rounded-md'>
                        {provider.icon ? (
                          getLobeIcon(provider.icon, 20)
                        ) : (
                          <span className='text-muted-foreground text-xs font-semibold'>
                            {provider.display_name.charAt(0).toUpperCase()}
                          </span>
                        )}
                      </div>
                      <div className='min-w-0 flex-1'>
                        <div className='truncate text-sm font-medium'>
                          {provider.display_name}
                        </div>
                        <div className='text-muted-foreground truncate font-mono text-xs'>
                          {provider.slug}
                        </div>
                      </div>
                    </CommandItem>
                  ))}
                </CommandGroup>
              </CommandList>
            </Command>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}

export function ModelProvidersSubtable(props: { model: Model }) {
  const { t } = useTranslation()
  const [selectedProvider, setSelectedProvider] = useState<Provider | null>(
    null
  )
  const [providerPickerOpen, setProviderPickerOpen] = useState(false)
  const [configOpen, setConfigOpen] = useState(false)

  const providersQuery = useQuery({
    queryKey: providersQueryKeys.list({ all: true }),
    queryFn: getAllProviders,
    staleTime: 0,
    refetchOnMount: 'always',
  })
  const pricesQuery = useQuery({
    queryKey: modelProviderPriceQueryKeys.list(props.model.id),
    queryFn: () => getModelProviderPrices(props.model.id),
    staleTime: 0,
    refetchOnMount: 'always',
  })

  const providers = providersQuery.data ?? EMPTY_PROVIDERS
  const prices = pricesQuery.data?.data ?? EMPTY_PRICES
  const providersBySlug = useMemo(
    () => new Map(providers.map((provider) => [provider.slug, provider])),
    [providers]
  )
  const pricesBySlug = useMemo(
    () => new Map(prices.map((price) => [price.provider_slug, price])),
    [prices]
  )
  const configuredProviders = useMemo(
    () =>
      prices.map((price) => {
        const provider = providersBySlug.get(price.provider_slug)
        return {
          price,
          provider:
            provider ?? {
              id: 0,
              slug: price.provider_slug,
              display_name: price.provider_slug,
              byok_supported: false,
              status: 0,
              created_time: 0,
              updated_time: 0,
            },
        }
      }),
    [prices, providersBySlug]
  )

  const openProvider = (provider: Provider) => {
    setSelectedProvider(provider)
    setConfigOpen(true)
  }

  const configuredCount = prices?.length ?? 0
  const unconfiguredProviders = useMemo(
    () => providers.filter((provider) => !pricesBySlug.has(provider.slug)),
    [providers, pricesBySlug]
  )

  return (
    <div className='bg-muted/20 space-y-2 p-2 sm:p-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='min-w-0'>
          <div className='text-sm font-semibold'>{t('Providers')}</div>
          <div className='text-muted-foreground text-xs'>
            {t('Configured')}: {configuredCount}
          </div>
        </div>
        <Button
          type='button'
          size='sm'
          variant='outline'
          className='h-7 gap-1 px-2 text-xs'
          disabled={unconfiguredProviders.length === 0}
          onClick={() => setProviderPickerOpen(true)}
        >
          <Plus className='size-3.5' />
          {t('Add provider configuration')}
        </Button>
      </div>

      {configuredProviders.length === 0 ? (
        <div className='text-muted-foreground rounded-md border border-dashed px-3 py-4 text-center text-sm'>
          {t('Not configured')}
        </div>
      ) : (
        <div className='min-w-0 overflow-x-auto rounded-md border [&_td]:text-xs! [&_th]:text-xs! [&_td_*]:text-xs! [&_th_*]:text-xs!'>
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
                <th className='w-[180px] px-2 py-1.5 text-left font-medium'>
                  {t('Provider')}
                </th>
                <th className='w-[96px] px-2 py-1.5 text-left font-medium'>
                  {t('Status')}
                </th>
                <th className='w-[180px] px-2 py-1.5 text-left font-medium'>
                  {t('Provider model name')}
                </th>
                <th className='w-[120px] px-2 py-1.5 text-right font-medium'>
                  {t('Input')}
                </th>
                <th className='w-[120px] px-2 py-1.5 text-right font-medium'>
                  {t('Output')}
                </th>
                <th className='w-[100px] px-2 py-1.5 text-right font-medium'>
                  {t('Cache')}
                </th>
                <th className='w-[70px] px-2 py-1.5 text-right font-medium'>
                  {t('Discount')}
                </th>
                <th className='w-[96px] px-2 py-1.5 text-right font-medium'>
                  {t('Actions')}
                </th>
              </tr>
            </thead>
            <tbody className='divide-y'>
              {configuredProviders.map(({ price, provider }) => {
                const icon = provider.icon ? getLobeIcon(provider.icon, 18) : null
                return (
                  <tr key={price.id || provider.slug}>
                    <td className='min-w-0 px-2 py-1.5'>
                      <div className='flex min-w-0 items-center gap-2'>
                        <div className='bg-muted flex size-6 shrink-0 items-center justify-center rounded-md'>
                          {icon || (
                            <span className='text-muted-foreground text-xs font-semibold'>
                              {provider.display_name.charAt(0).toUpperCase()}
                            </span>
                          )}
                        </div>
                        <div className='min-w-0'>
                          <div className='truncate font-medium'>
                            {provider.display_name}
                          </div>
                          <div className='text-muted-foreground font-mono text-xs'>
                            {provider.slug}
                          </div>
                        </div>
                      </div>
                    </td>
                    <td className='px-2 py-1.5'>
                      <div className='flex flex-wrap gap-1'>
                        <span className='bg-muted rounded px-1 py-0.5 text-[11px]'>
                          {provider.status === 1
                            ? t('Enabled')
                            : t('Disabled')}
                        </span>
                      </div>
                    </td>
                    <td className='max-w-44 min-w-0 px-2 py-1.5 font-mono text-[11px] leading-snug break-all whitespace-normal'>
                      {price?.model_name || '—'}
                    </td>
                    <td className='px-2 py-1.5 text-right'>
                      <ProviderPriceCell
                        price={price}
                        field='input_price'
                        listField='input_list_price'
                      />
                    </td>
                    <td className='px-2 py-1.5 text-right'>
                      <ProviderPriceCell
                        price={price}
                        field='output_price'
                        listField='output_list_price'
                      />
                    </td>
                    <td className='px-2 py-1.5 text-right'>
                      <ProviderPriceCell
                        price={price}
                        field='cache_read_price'
                        listField='cache_read_list_price'
                      />
                    </td>
                    <td className='px-2 py-1.5 text-right'>
                      <ProviderDiscountCell price={price} />
                    </td>
                    <td className='px-2 py-1.5 text-right'>
                      <Button
                        type='button'
                        variant='ghost'
                        size='sm'
                        className='h-7 gap-1 px-1.5 text-[11px]'
                        onClick={() => openProvider(provider)}
                      >
                        <Pencil className='size-3.5' />
                        {t('Edit')}
                      </Button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      <ProviderConfigAddDrawer
        open={providerPickerOpen}
        onOpenChange={setProviderPickerOpen}
        providers={unconfiguredProviders}
        onSelect={openProvider}
      />
      <ModelProviderConfigDrawer
        open={configOpen}
        onOpenChange={(open) => {
          setConfigOpen(open)
          if (!open) setSelectedProvider(null)
        }}
        modelId={props.model.id}
        modelName={props.model.model_name}
        providerSlug={selectedProvider?.slug}
        providerName={selectedProvider?.display_name}
      />
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

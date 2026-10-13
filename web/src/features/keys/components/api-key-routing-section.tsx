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
import { Route } from 'lucide-react'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
  sideDrawerSwitchItemClassName,
} from '@/components/drawer-layout'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'

import {
  ROUTING_MODES,
  ROUTING_MODE_LABELS,
  type RoutingPreferenceFormValues,
} from '../lib'

type ApiKeyRoutingSectionProps = {
  form: UseFormReturn<RoutingPreferenceFormValues>
}

/**
 * Token-level Scheduler routing preference. It owns a separate form instance
 * because it is persisted through its own endpoint
 * (`PUT /api/token/:id/routing-preference`), not the token payload.
 */
export function ApiKeyRoutingSection({ form }: ApiKeyRoutingSectionProps) {
  const { t } = useTranslation()
  const enabled = form.watch('enabled')

  return (
    <Form {...form}>
      <SideDrawerSection>
        <SideDrawerSectionHeader
          title={t('Routing Preference')}
          description={t(
            'How the Scheduler selects providers for requests made with this API key'
          )}
          icon={<Route className='size-4' />}
        />

        <FormField
          control={form.control}
          name='enabled'
          render={({ field }) => (
            <FormItem className={sideDrawerSwitchItemClassName()}>
              <div className='flex flex-col gap-0.5'>
                <FormLabel className='text-sm'>
                  {t('Custom routing preference')}
                </FormLabel>
                <FormDescription className='text-xs'>
                  {t(
                    'When disabled, this API key uses the platform default routing policy.'
                  )}
                </FormDescription>
              </div>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />

        {enabled && (
          <FormField
            control={form.control}
            name='mode'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Provider sort')}</FormLabel>
                <Select
                  items={ROUTING_MODES.map((mode) => ({
                    value: mode,
                    label: t(ROUTING_MODE_LABELS[mode]),
                  }))}
                  value={field.value}
                  onValueChange={field.onChange}
                >
                  <FormControl>
                    <SelectTrigger className='w-full'>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    {ROUTING_MODES.map((mode) => (
                      <SelectItem key={mode} value={mode}>
                        {t(ROUTING_MODE_LABELS[mode])}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FormDescription>
                  {t('Applies to every model requested with this API key.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        )}
      </SideDrawerSection>
    </Form>
  )
}

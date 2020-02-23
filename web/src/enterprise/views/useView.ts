import { useState, useMemo } from 'react'
import { Observable, combineLatest } from 'rxjs'
import { Contributions, ViewContribution, FormContribution, Evaluated } from '../../../../shared/src/api/protocol'
import { useObservable } from '../../util/useObservable'
import { map, tap } from 'rxjs/operators'
import {
    PanelViewWithComponent,
    ViewProviderRegistry,
    ViewProviderRegistrationOptions,
} from '../../../../shared/src/api/client/services/view'

interface ViewData {
    /**
     * The view.
     */
    view: ViewContribution

    /**
     * The form, `undefined` if none is defined, or `null` if a form is defined but not found.
     */
    form?: FormContribution | null

    /**
     * The view's associated panel views, if any.
     */
    panelViews?: (PanelViewWithComponent & Pick<ViewProviderRegistrationOptions, 'id'>)[]
}

/**
 * A React hook that returns the view and its associated contents (such as its form, if any),
 * `undefined` if loading, and `null` if not found.
 */
export const useView = (
    viewID: string,
    contributions: Observable<Evaluated<Contributions>>,
    viewProviderRegistry: ViewProviderRegistry
): ViewData | undefined | null => {
    const [result, setResult] = useState<ViewData | undefined | null>()

    useObservable(
        useMemo(
            () =>
                combineLatest([
                    contributions.pipe(
                        map(contributions => {
                            const view = contributions.views?.find(({ id }) => id === viewID)
                            if (!view) {
                                return null
                            }
                            if (!view.form) {
                                return { view }
                            }
                            const form = contributions.forms?.find(({ id }) => id === view.form) ?? null
                            return { view, form }
                        })
                    ),
                    viewProviderRegistry.getView(viewID).pipe(map(panelView => (panelView ? [panelView] : undefined))),
                ]).pipe(
                    tap(([viewAndForm, panelViews]) => setResult(viewAndForm ? { ...viewAndForm, panelViews } : null))
                ),
            [contributions, viewID, viewProviderRegistry]
        )
    )

    return result
}

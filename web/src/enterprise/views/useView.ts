import { useState, useMemo } from 'react'
import { Observable } from 'rxjs'
import { Contributions, ViewContribution, FormContribution, Evaluated } from '../../../../shared/src/api/protocol'
import { useObservable } from '../../util/useObservable'
import { map, tap } from 'rxjs/operators'

interface ViewAndFormContribution {
    /**
     * The view.
     */
    view: ViewContribution

    /**
     * The form, `undefined` if none is defined, or `null` if a form is defined but not found.
     */
    form?: FormContribution | null
}

/**
 * A React hook that returns the view and its associated contents (such as its form, if any),
 * `undefined` if loading, and `null` if not found.
 */
export const useView = (
    viewID: string,
    contributions: Observable<Evaluated<Contributions>>
): ViewAndFormContribution | undefined | null => {
    const [result, setResult] = useState<ViewAndFormContribution | undefined | null>()

    useObservable(
        useMemo(
            () =>
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
                    }),
                    tap(setResult)
                ),
            [contributions, viewID]
        )
    )

    return result
}

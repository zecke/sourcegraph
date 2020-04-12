import ErrorIcon from 'mdi-react/ErrorIcon'
import ExternalLinkIcon from 'mdi-react/ExternalLinkIcon'
import React, { useState, useMemo, useEffect, useCallback } from 'react'
import { Observable, Subject } from 'rxjs'
import { catchError, map, mapTo, startWith, switchMap, tap } from 'rxjs/operators'
import { gql } from '../../../../../../shared/src/graphql/graphql'
import * as GQL from '../../../../../../shared/src/graphql/schema'
import { asError, createAggregateError, ErrorLike, isErrorLike } from '../../../../../../shared/src/util/errors'
import { mutateGraphQL } from '../../../../backend/graphql'

interface Props {
    /** The product subscription to show a billing link for. */
    productSubscription: Pick<GQL.IProductSubscription, 'id' | 'urlForSiteAdminBilling'>

    /** Called when the product subscription is updated. */
    onDidUpdate: () => void
}

const LOADING = 'loading' as const

/**
 * SiteAdminProductSubscriptionBillingLink shows a link to the product subscription on the billing system, if there
 * is an associated billing record. It also supports setting or unsetting the association with the billing system.
 */
export const SiteAdminProductSubscriptionBillingLink: React.FunctionComponent<Props> = ({
    productSubscription,
    onDidUpdate,
}) => {
    /** The result of updating this subscription: null for done or not started, loading, or an error. */
    const [updateOrError, setUpdateOrError] = useState<typeof LOADING | null | ErrorLike>(null)

    const updates = useMemo(() => new Subject<{ id: GQL.ID; billingSubscriptionID: string | null }>(), [])
    useEffect(() => {
        const subscription = updates
            .pipe(
                switchMap(({ id, billingSubscriptionID }) =>
                    setProductSubscriptionBilling({ id, billingSubscriptionID }).pipe(
                        mapTo(null),
                        tap(() => onDidUpdate()),
                        catchError((err: ErrorLike) => [asError(err)]),
                        startWith(LOADING)
                    )
                )
            )
            .subscribe(setUpdateOrError)
        return () => subscription.unsubscribe()
    }, [onDidUpdate, updates])

    const onLinkBillingClick = useCallback(() => {
        const billingSubscriptionID = window.prompt('Enter new Stripe billing subscription ID:', 'sub_ABCDEF12345678')

        // Ignore if the user pressed "Cancel" or did not enter any value.
        if (!billingSubscriptionID) {
            return
        }

        updates.next({ id: productSubscription.id, billingSubscriptionID })
    }, [productSubscription.id, updates])

    const onUnlinkBillingClick = useCallback(
        () => updates.next({ id: productSubscription.id, billingSubscriptionID: null }),
        [productSubscription.id, updates]
    )

    const productSubscriptionHasLinkedBilling = productSubscription.urlForSiteAdminBilling !== null
    return (
        <div className="site-admin-product-subscription-billing-link">
            <div className="d-flex align-items-center">
                {productSubscription.urlForSiteAdminBilling && (
                    <a href={productSubscription.urlForSiteAdminBilling} className="mr-2 d-flex align-items-center">
                        View billing subscription <ExternalLinkIcon className="icon-inline ml-1" />
                    </a>
                )}
                {isErrorLike(updateOrError) && (
                    <ErrorIcon className="icon-inline text-danger mr-2" data-tooltip={updateOrError.message} />
                )}
                <button
                    type="button"
                    className="btn btn-secondary btn-sm"
                    onClick={productSubscriptionHasLinkedBilling ? onUnlinkBillingClick : onLinkBillingClick}
                    disabled={updateOrError === LOADING}
                >
                    {productSubscriptionHasLinkedBilling ? 'Unlink' : 'Link billing subscription'}
                </button>
            </div>
        </div>
    )
}

function setProductSubscriptionBilling(
    args: GQL.ISetProductSubscriptionBillingOnDotcomMutationArguments
): Observable<void> {
    return mutateGraphQL(
        gql`
            mutation SetProductSubscriptionBilling($id: ID!, $billingSubscriptionID: String) {
                dotcom {
                    setProductSubscriptionBilling(id: $id, billingSubscriptionID: $billingSubscriptionID) {
                        alwaysNil
                    }
                }
            }
        `,
        args
    ).pipe(
        map(({ data, errors }) => {
            if (!data || !data.dotcom || !data.dotcom.setProductSubscriptionBilling || (errors && errors.length > 0)) {
                throw createAggregateError(errors)
            }
        })
    )
}

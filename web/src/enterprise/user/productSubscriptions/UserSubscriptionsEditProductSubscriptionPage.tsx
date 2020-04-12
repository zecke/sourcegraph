import { LoadingSpinner } from '@sourcegraph/react-loading-spinner'
import ArrowLeftIcon from 'mdi-react/ArrowLeftIcon'
import React, { useState, useEffect, useMemo, useCallback } from 'react'
import { RouteComponentProps } from 'react-router'
import { Link } from 'react-router-dom'
import { Observable, Subject, throwError } from 'rxjs'
import { catchError, map, mapTo, startWith, switchMap, tap } from 'rxjs/operators'
import { gql } from '../../../../../shared/src/graphql/graphql'
import * as GQL from '../../../../../shared/src/graphql/schema'
import { asError, createAggregateError, ErrorLike, isErrorLike } from '../../../../../shared/src/util/errors'
import { mutateGraphQL, queryGraphQL } from '../../../backend/graphql'
import { PageTitle } from '../../../components/PageTitle'
import { eventLogger } from '../../../tracking/eventLogger'
import { ProductSubscriptionForm, ProductSubscriptionFormData } from './ProductSubscriptionForm'
import { ThemeProps } from '../../../../../shared/src/theme'
import { ErrorAlert } from '../../../components/alerts'

interface Props extends RouteComponentProps<{ subscriptionUUID: string }>, ThemeProps {
    user: Pick<GQL.IUser, 'id'>

    /** For mocking in tests only. */
    _queryProductSubscription?: typeof queryProductSubscription
}

type ProductSubscription = Pick<GQL.IProductSubscription, 'id' | 'name' | 'invoiceItem' | 'url'>

const LOADING = 'loading' as const

/**
 * Displays a page for editing a product subscription in the user subscriptions area.
 */
export const UserSubscriptionsEditProductSubscriptionPage: React.FunctionComponent<Props> = ({
    user,
    match,
    history,
    isLightTheme,
    _queryProductSubscription = queryProductSubscription,
}) => {
    useEffect(() => eventLogger.logViewEvent('UserSubscriptionsEditProductSubscription'), [])

    /**
     * The product subscription, or loading, or an error.
     */
    const [productSubscriptionOrError, setProductSubscriptionOrError] = useState<
        typeof LOADING | ProductSubscription | ErrorLike
    >(LOADING)
    const subscriptionUUID = match.params.subscriptionUUID
    useEffect(() => {
        const subscription = _queryProductSubscription(subscriptionUUID)
            .pipe(
                catchError((err: ErrorLike) => [asError(err)]),
                startWith(LOADING)
            )
            .subscribe(setProductSubscriptionOrError)
        return () => subscription.unsubscribe()
    }, [_queryProductSubscription, subscriptionUUID])

    /**
     * The result of updating the paid product subscription: null when complete or not started yet,
     * loading, or an error.
     */
    const [updateOrError, setUpdateOrError] = useState<null | typeof LOADING | ErrorLike>(null)

    const submits = useMemo(() => new Subject<ProductSubscriptionFormData>(), [])
    useEffect(() => {
        const subscription = submits
            .pipe(
                switchMap(args => {
                    const subscriptionID =
                        productSubscriptionOrError !== LOADING && !isErrorLike(productSubscriptionOrError)
                            ? productSubscriptionOrError.id
                            : null
                    if (subscriptionID === null) {
                        return throwError(new Error('no product subscription'))
                    }
                    return updatePaidProductSubscription({
                        update: args.productSubscription,
                        subscriptionID,
                        paymentToken: args.paymentToken,
                    }).pipe(
                        tap(({ productSubscription }) => {
                            // Redirect back to subscription upon success.
                            history.push(productSubscription.url)
                        }),
                        mapTo(null),
                        startWith(LOADING)
                    )
                }),
                catchError((err: ErrorLike) => [asError(err)])
            )
            .subscribe(setUpdateOrError)
        return () => subscription.unsubscribe()
    }, [history, productSubscriptionOrError, submits])
    const onSubmit = useCallback(
        (args: ProductSubscriptionFormData): void => {
            submits.next(args)
        },
        [submits]
    )

    return (
        <div className="user-subscriptions-edit-product-subscription-page">
            <PageTitle title="Edit subscription" />
            {productSubscriptionOrError === LOADING ? (
                <LoadingSpinner className="icon-inline" />
            ) : isErrorLike(productSubscriptionOrError) ? (
                <ErrorAlert className="my-2" error={productSubscriptionOrError} />
            ) : (
                <>
                    <Link to={productSubscriptionOrError.url} className="btn btn-link btn-sm mb-3">
                        <ArrowLeftIcon className="icon-inline" /> Subscription
                    </Link>
                    <h2>Upgrade or change subscription {productSubscriptionOrError.name}</h2>
                    <ProductSubscriptionForm
                        accountID={user.id}
                        subscriptionID={productSubscriptionOrError.id}
                        isLightTheme={isLightTheme}
                        onSubmit={onSubmit}
                        submissionState={updateOrError}
                        initialValue={
                            productSubscriptionOrError.invoiceItem
                                ? {
                                      billingPlanID: productSubscriptionOrError.invoiceItem.plan.billingPlanID,
                                      userCount: productSubscriptionOrError.invoiceItem.userCount,
                                  }
                                : undefined
                        }
                        primaryButtonText="Upgrade subscription"
                        afterPrimaryButton={
                            <small className="form-text text-muted">
                                An upgraded license key will be available immediately after payment.
                            </small>
                        }
                    />
                </>
            )}
        </div>
    )
}

function queryProductSubscription(uuid: string): Observable<ProductSubscription> {
    return queryGraphQL(
        gql`
            query ProductSubscription($uuid: String!) {
                dotcom {
                    productSubscription(uuid: $uuid) {
                        ...ProductSubscriptionFields
                    }
                }
            }

            fragment ProductSubscriptionFields on ProductSubscription {
                id
                name
                invoiceItem {
                    plan {
                        billingPlanID
                    }
                    userCount
                    expiresAt
                }
                url
            }
        `,
        { uuid }
    ).pipe(
        map(({ data, errors }) => {
            if (!data || !data.dotcom || !data.dotcom.productSubscription || (errors && errors.length > 0)) {
                throw createAggregateError(errors)
            }
            return data.dotcom.productSubscription
        })
    )
}

function updatePaidProductSubscription(
    args: GQL.IUpdatePaidProductSubscriptionOnDotcomMutationArguments
): Observable<GQL.IUpdatePaidProductSubscriptionResult> {
    return mutateGraphQL(
        gql`
            mutation UpdatePaidProductSubscription(
                $subscriptionID: ID!
                $update: ProductSubscriptionInput!
                $paymentToken: String!
            ) {
                dotcom {
                    updatePaidProductSubscription(
                        subscriptionID: $subscriptionID
                        update: $update
                        paymentToken: $paymentToken
                    ) {
                        productSubscription {
                            url
                        }
                    }
                }
            }
        `,
        args
    ).pipe(
        map(({ data, errors }) => {
            if (!data || !data.dotcom || !data.dotcom.updatePaidProductSubscription || (errors && errors.length > 0)) {
                throw createAggregateError(errors)
            }
            return data.dotcom.updatePaidProductSubscription
        })
    )
}

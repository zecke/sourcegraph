import { LoadingSpinner } from '@sourcegraph/react-loading-spinner'
import { parseISO } from 'date-fns'
import React, { useState, useEffect } from 'react'
import { RouteComponentProps } from 'react-router'
import { Link } from 'react-router-dom'
import { Observable } from 'rxjs'
import { catchError, map, startWith } from 'rxjs/operators'
import { gql } from '../../../../../shared/src/graphql/graphql'
import * as GQL from '../../../../../shared/src/graphql/schema'
import { asError, createAggregateError, ErrorLike, isErrorLike } from '../../../../../shared/src/util/errors'
import { queryGraphQL } from '../../../backend/graphql'
import { PageTitle } from '../../../components/PageTitle'
import { SiteAdminAlert } from '../../../site-admin/SiteAdminAlert'
import { eventLogger } from '../../../tracking/eventLogger'
import { mailtoSales } from '../../productSubscription/helpers'
import { BackToAllSubscriptionsLink } from './BackToAllSubscriptionsLink'
import { ProductSubscriptionBilling } from './ProductSubscriptionBilling'
import { ProductSubscriptionHistory } from './ProductSubscriptionHistory'
import { UserProductSubscriptionStatus } from './UserProductSubscriptionStatus'
import { ErrorAlert } from '../../../components/alerts'

interface Props extends Pick<RouteComponentProps<{ subscriptionUUID: string }>, 'match'> {
    user: Pick<GQL.IUser, 'settingsURL'>

    /** For mocking in tests only. */
    _queryProductSubscription?: typeof queryProductSubscription
}

const LOADING = 'loading' as const

/**
 * Displays a product subscription in the user subscriptions area.
 */
export const UserSubscriptionsProductSubscriptionPage: React.FunctionComponent<Props> = ({
    user,
    match,
    _queryProductSubscription = queryProductSubscription,
}) => {
    useEffect(() => eventLogger.logViewEvent('UserSubscriptionsProductSubscription'), [])

    /**
     * The product subscription, or loading, or an error.
     */
    const [productSubscriptionOrError, setProductSubscriptionOrError] = useState<
        typeof LOADING | GQL.IProductSubscription | ErrorLike
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

    return (
        <div className="user-subscriptions-product-subscription-page">
            <PageTitle title="Subscription" />
            <div className="d-flex align-items-center justify-content-between">
                <BackToAllSubscriptionsLink user={user} />
                {productSubscriptionOrError !== LOADING &&
                    !isErrorLike(productSubscriptionOrError) &&
                    productSubscriptionOrError.urlForSiteAdmin && (
                        <SiteAdminAlert className="small m-0 p-1">
                            <Link to={productSubscriptionOrError.urlForSiteAdmin} className="mt-2 d-block">
                                View subscription
                            </Link>
                        </SiteAdminAlert>
                    )}
            </div>
            {productSubscriptionOrError === LOADING ? (
                <LoadingSpinner className="icon-inline" />
            ) : isErrorLike(productSubscriptionOrError) ? (
                <ErrorAlert className="my-2" error={productSubscriptionOrError} />
            ) : (
                <>
                    <h2>Subscription {productSubscriptionOrError.name}</h2>
                    {(productSubscriptionOrError.invoiceItem ||
                        (productSubscriptionOrError.activeLicense &&
                            productSubscriptionOrError.activeLicense.info)) && (
                        <UserProductSubscriptionStatus
                            subscriptionName={productSubscriptionOrError.name}
                            productNameWithBrand={
                                productSubscriptionOrError.activeLicense &&
                                productSubscriptionOrError.activeLicense.info
                                    ? productSubscriptionOrError.activeLicense.info.productNameWithBrand
                                    : productSubscriptionOrError.invoiceItem!.plan.nameWithBrand
                            }
                            userCount={
                                productSubscriptionOrError.activeLicense &&
                                productSubscriptionOrError.activeLicense.info
                                    ? productSubscriptionOrError.activeLicense.info.userCount
                                    : productSubscriptionOrError.invoiceItem!.userCount
                            }
                            expiresAt={
                                productSubscriptionOrError.activeLicense &&
                                productSubscriptionOrError.activeLicense.info
                                    ? parseISO(productSubscriptionOrError.activeLicense.info.expiresAt)
                                    : parseISO(productSubscriptionOrError.invoiceItem!.expiresAt)
                            }
                            licenseKey={
                                productSubscriptionOrError.activeLicense &&
                                productSubscriptionOrError.activeLicense.licenseKey
                            }
                        />
                    )}
                    <div className="card mt-3">
                        <div className="card-header">Billing</div>
                        {productSubscriptionOrError.invoiceItem ? (
                            <>
                                <ProductSubscriptionBilling productSubscription={productSubscriptionOrError} />
                                <div className="card-footer">
                                    <a
                                        href={mailtoSales({
                                            subject: `Change payment method for subscription ${productSubscriptionOrError.name}`,
                                        })}
                                    >
                                        Contact sales
                                    </a>{' '}
                                    to change your payment method.
                                </div>
                            </>
                        ) : (
                            <div className="card-body">
                                <span className="text-muted ">
                                    No billing information is associated with this subscription.{' '}
                                    <a
                                        href={mailtoSales({
                                            subject: `Billing for subscription ${productSubscriptionOrError.name}`,
                                        })}
                                    >
                                        Contact sales
                                    </a>{' '}
                                    for help.
                                </span>
                            </div>
                        )}
                    </div>
                    <div className="card mt-3">
                        <div className="card-header">History</div>
                        <ProductSubscriptionHistory productSubscription={productSubscriptionOrError} />
                    </div>
                </>
            )}
        </div>
    )
}

function queryProductSubscription(uuid: string): Observable<GQL.IProductSubscription> {
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
                account {
                    id
                    username
                    displayName
                    emails {
                        email
                        verified
                    }
                }
                invoiceItem {
                    plan {
                        billingPlanID
                        name
                        nameWithBrand
                        pricePerUserPerYear
                    }
                    userCount
                    expiresAt
                }
                events {
                    id
                    date
                    title
                    description
                    url
                }
                activeLicense {
                    licenseKey
                    info {
                        productNameWithBrand
                        tags
                        userCount
                        expiresAt
                    }
                }
                createdAt
                isArchived
                url
                urlForSiteAdmin
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

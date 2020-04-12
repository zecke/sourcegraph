import { LoadingSpinner } from '@sourcegraph/react-loading-spinner'
import { parseISO } from 'date-fns'
import formatDistanceStrict from 'date-fns/formatDistanceStrict'
import { isEqual } from 'lodash'
import ErrorIcon from 'mdi-react/ErrorIcon'
import React, { useState, useEffect } from 'react'
import { Observable } from 'rxjs'
import { catchError, map, startWith } from 'rxjs/operators'
import { gql } from '../../../../../shared/src/graphql/graphql'
import * as GQL from '../../../../../shared/src/graphql/schema'
import { asError, createAggregateError, ErrorLike, isErrorLike } from '../../../../../shared/src/util/errors'
import { numberWithCommas } from '../../../../../shared/src/util/strings'
import { queryGraphQL } from '../../../backend/graphql'
import { formatUserCount, mailtoSales } from '../../productSubscription/helpers'
import { ProductSubscriptionBeforeAfterInvoiceItem } from './ProductSubscriptionBeforeAfterInvoiceItem'

interface Props {
    /**
     * The ID of the account associated with the subscription, or null if there is none (in which case the
     * subscription price can be quoted but the subscription can't be bought).
     */
    accountID: GQL.ID | null

    /** The existing product subscription to edit, or null if this is a new subscription. */
    subscriptionID: GQL.ID | null

    /**
     * The product subscription chosen by the user, or null for an invalid choice.
     */
    productSubscription: GQL.IProductSubscriptionInput | null

    /**
     * Called when the validity state of the payment and billing information changes. Initially it
     * is always false.
     */
    onValidityChange: (value: boolean) => void

    /** For mocking in tests only. */
    _queryPreviewProductSubscriptionInvoice?: typeof queryPreviewProductSubscriptionInvoice
}

const LOADING = 'loading' as const

type PreviewInvoiceOrError = GQL.IProductSubscriptionPreviewInvoice | null | typeof LOADING | ErrorLike

const isPreviewInvoiceInvalid = (previewInvoiceOrError: PreviewInvoiceOrError): boolean =>
    Boolean(
        previewInvoiceOrError === null ||
            previewInvoiceOrError === LOADING ||
            isErrorLike(previewInvoiceOrError) ||
            isEqual(previewInvoiceOrError.beforeInvoiceItem, previewInvoiceOrError.afterInvoiceItem) ||
            previewInvoiceOrError.isDowngradeRequiringManualIntervention
    )

/**
 * Displays the payment section of the new product subscription form.
 */
export const NewProductSubscriptionPaymentSection: React.FunctionComponent<Props> = ({
    accountID,
    subscriptionID,
    productSubscription,
    onValidityChange,
    _queryPreviewProductSubscriptionInvoice = queryPreviewProductSubscriptionInvoice,
}) => {
    /**
     * The preview invoice for the subscription, null if the input is invalid to generate an
     * invoice, loading, or an error.
     */
    const [previewInvoiceOrError, setPreviewInvoiceOrError] = useState<PreviewInvoiceOrError>(LOADING)

    useEffect(() => {
        onValidityChange(!isPreviewInvoiceInvalid(previewInvoiceOrError))
    }, [onValidityChange, previewInvoiceOrError])

    useEffect(() => {
        if (productSubscription === null) {
            setPreviewInvoiceOrError(null)
            return
        }

        const subscription = _queryPreviewProductSubscriptionInvoice({
            account: accountID,
            subscriptionToUpdate: subscriptionID,
            productSubscription,
        })
            .pipe(
                catchError((err: ErrorLike) => [asError(err)]),
                startWith(LOADING)
            )
            .subscribe(setPreviewInvoiceOrError)
        return () => subscription.unsubscribe()
    }, [accountID, subscriptionID, productSubscription, _queryPreviewProductSubscriptionInvoice])

    return (
        <div className="new-product-subscription-payment-section">
            <div className="form-text mb-2">
                {previewInvoiceOrError === LOADING ? (
                    <LoadingSpinner className="icon-inline" />
                ) : !productSubscription || previewInvoiceOrError === null ? (
                    <>&mdash;</>
                ) : isErrorLike(previewInvoiceOrError) ? (
                    <span className="text-danger">
                        <ErrorIcon className="icon-inline" data-tooltip={previewInvoiceOrError.message} /> Error
                    </span>
                ) : previewInvoiceOrError.beforeInvoiceItem ? (
                    <>
                        <ProductSubscriptionBeforeAfterInvoiceItem
                            beforeInvoiceItem={previewInvoiceOrError.beforeInvoiceItem}
                            afterInvoiceItem={previewInvoiceOrError.afterInvoiceItem}
                            className="mb-2"
                        />
                        {previewInvoiceOrError.isDowngradeRequiringManualIntervention ? (
                            <div className="alert alert-danger mb-2">
                                Self-service downgrades are not yet supported.{' '}
                                <a
                                    href={mailtoSales({
                                        // eslint-disable-next-line @typescript-eslint/no-non-null-assertion
                                        subject: `Downgrade subscription ${subscriptionID!}`,
                                    })}
                                >
                                    Contact sales
                                </a>{' '}
                                for help.
                            </div>
                        ) : (
                            !isEqual(
                                previewInvoiceOrError.beforeInvoiceItem,
                                previewInvoiceOrError.afterInvoiceItem
                            ) && (
                                <div className="mb-2">
                                    Amount due: ${numberWithCommas(previewInvoiceOrError.price / 100)}
                                </div>
                            )
                        )}
                    </>
                ) : (
                    <>
                        Total: ${numberWithCommas(previewInvoiceOrError.price / 100)} for{' '}
                        {formatDistanceStrict(parseISO(previewInvoiceOrError.afterInvoiceItem.expiresAt), Date.now())} (
                        {formatUserCount(productSubscription.userCount)})
                        {/* Include invisible LoadingSpinner to ensure that the height remains constant between loading and total. */}
                        <LoadingSpinner className="icon-inline invisible" />
                    </>
                )}
            </div>
        </div>
    )
}

function queryPreviewProductSubscriptionInvoice(
    args: GQL.IPreviewProductSubscriptionInvoiceOnDotcomQueryArguments
): Observable<GQL.IProductSubscriptionPreviewInvoice> {
    return queryGraphQL(
        gql`
            query PreviewProductSubscriptionInvoice(
                $account: ID!
                $subscriptionToUpdate: ID
                $productSubscription: ProductSubscriptionInput!
            ) {
                dotcom {
                    previewProductSubscriptionInvoice(
                        account: $account
                        subscriptionToUpdate: $subscriptionToUpdate
                        productSubscription: $productSubscription
                    ) {
                        price
                        prorationDate
                        isDowngradeRequiringManualIntervention
                        beforeInvoiceItem {
                            plan {
                                billingPlanID
                                name
                                pricePerUserPerYear
                            }
                            userCount
                            expiresAt
                        }
                        afterInvoiceItem {
                            plan {
                                billingPlanID
                                name
                                pricePerUserPerYear
                            }
                            userCount
                            expiresAt
                        }
                    }
                }
            }
        `,
        args
    ).pipe(
        map(({ data, errors }) => {
            if (
                !data ||
                !data.dotcom ||
                !data.dotcom.previewProductSubscriptionInvoice ||
                (errors && errors.length > 0)
            ) {
                throw createAggregateError(errors)
            }
            return data.dotcom.previewProductSubscriptionInvoice
        })
    )
}

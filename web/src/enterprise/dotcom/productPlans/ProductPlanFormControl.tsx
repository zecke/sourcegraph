import { LoadingSpinner } from '@sourcegraph/react-loading-spinner'
import React, { useState, useCallback, useEffect } from 'react'
import { Observable } from 'rxjs'
import { catchError, map, startWith, tap } from 'rxjs/operators'
import { gql } from '../../../../../shared/src/graphql/graphql'
import * as GQL from '../../../../../shared/src/graphql/schema'
import { asError, createAggregateError, ErrorLike, isErrorLike } from '../../../../../shared/src/util/errors'
import { queryGraphQL } from '../../../backend/graphql'
import { ProductPlanPrice } from './ProductPlanPrice'
import { ProductPlanTiered } from './ProductPlanTiered'
import { ErrorAlert } from '../../../components/alerts'

interface Props {
    /** The selected plan's billing ID. */
    value: string | null

    /** Called when the selected plan changes (with its billing ID). */
    onChange: (value: string | null) => void

    disabled?: boolean
    className?: string

    /** For mocking in tests only. */
    _queryProductPlans?: typeof queryProductPlans
}

const LOADING = 'loading' as const

/**
 * Displays a form group for selecting a product plan.
 */
export const ProductPlanFormControl: React.FunctionComponent<Props> = ({
    value,
    onChange,
    disabled,
    className = '',
    _queryProductPlans = queryProductPlans,
}) => {
    /**
     * The list of all possible product plans.
     */
    const [plansOrError, setPlansOrError] = useState<GQL.IProductPlan[] | typeof LOADING | ErrorLike>(LOADING)

    useEffect(() => {
        const subscription = _queryProductPlans()
            .pipe(
                tap(plans => {
                    // If no plan is selected, select the 1st plan when the plans have loaded.
                    if (plans.length > 0 && value === null) {
                        onChange(plans[0].billingPlanID)
                    }
                }),
                catchError((err: ErrorLike) => [asError(err)]),
                startWith(LOADING)
            )
            .subscribe(setPlansOrError)
        return () => subscription.unsubscribe()
    }, [_queryProductPlans, onChange, value])

    const onPlanChange = useCallback<React.ChangeEventHandler<HTMLInputElement>>(
        e => {
            onChange(e.currentTarget.value)
        },
        [onChange]
    )

    const disableInputs = disabled || plansOrError === LOADING || isErrorLike(plansOrError)

    return (
        <div className={`product-plan-form-control ${className}`}>
            {plansOrError === LOADING ? (
                <LoadingSpinner className="icon-inline" />
            ) : isErrorLike(plansOrError) ? (
                <ErrorAlert error={plansOrError.message} />
            ) : (
                <>
                    <div className="list-group">
                        {plansOrError.map((plan, i) => (
                            <div key={plan.billingPlanID} className="list-group-item p-0">
                                <label className="p-3 mb-0 d-flex" htmlFor={`product-plan-form-control__plan${i}`}>
                                    <input
                                        type="radio"
                                        name="product-plan-form-control__plan"
                                        className="mr-2"
                                        id={`product-plan-form-control__plan${i}`}
                                        value={plan.billingPlanID}
                                        onChange={onPlanChange}
                                        required={true}
                                        disabled={disableInputs}
                                        checked={plan.billingPlanID === value}
                                    />
                                    <div>
                                        <strong>{plan.name}</strong>
                                        <div className="text-muted">
                                            {plan.planTiers.length > 0 ? (
                                                <ProductPlanTiered
                                                    planTiers={plan.planTiers}
                                                    tierMode={plan.tiersMode}
                                                    minQuantity={plan.minQuantity}
                                                />
                                            ) : (
                                                <ProductPlanPrice pricePerUserPerYear={plan.pricePerUserPerYear} />
                                            )}
                                        </div>
                                    </div>
                                </label>
                            </div>
                        ))}
                    </div>
                    <a
                        href="https://about.sourcegraph.com/pricing"
                        // eslint-disable-next-line react/jsx-no-target-blank
                        target="_blank"
                        rel="noopener"
                        className="small"
                    >
                        Compare plans
                    </a>
                </>
            )}
        </div>
    )
}

function queryProductPlans(): Observable<GQL.IProductPlan[]> {
    return queryGraphQL(
        gql`
            query ProductPlans {
                dotcom {
                    productPlans {
                        productPlanID
                        billingPlanID
                        name
                        pricePerUserPerYear
                        minQuantity
                        tiersMode
                        planTiers {
                            unitAmount
                            upTo
                            flatAmount
                        }
                    }
                }
            }
        `
    ).pipe(
        map(({ data, errors }) => {
            if (!data || !data.dotcom || !data.dotcom.productPlans || (errors && errors.length > 0)) {
                throw createAggregateError(errors)
            }
            return data.dotcom.productPlans
        })
    )
}

import { Subscription, Unsubscribable } from 'rxjs'
import { ExtensionsControllerProps } from '../../../../../shared/src/extensions/controller'
import { parseContributionExpressions } from '../../../../../shared/src/api/client/services/contribution'

export function registerSearchHelperSampleContributions({
    extensionsController,
}: ExtensionsControllerProps<'services'>): Unsubscribable {
    const subscriptions = new Subscription()

    const FORM_ID = 'searchHelperSample.form'
    subscriptions.add(
        extensionsController.services.contribution.registerContributions({
            contributions: parseContributionExpressions({
                forms: [
                    {
                        id: FORM_ID,
                        schema: {
                            type: 'object',
                            additionalProperties: false,
                            properties: {
                                query: {
                                    type: 'string',
                                },
                                version: {
                                    type: 'string',
                                    enum: ['v18', 'v19', 'v20', 'v21', 'latest'],
                                },
                            },
                        },
                    },
                ],
                views: [{ id: 'searchHelperSample.view', form: FORM_ID }],
            }),
        })
    )

    return subscriptions
}

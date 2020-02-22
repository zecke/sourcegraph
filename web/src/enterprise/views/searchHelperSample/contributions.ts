import { Subscription, Unsubscribable } from 'rxjs'
import { ExtensionsControllerProps } from '../../../../../shared/src/extensions/controller'
import { parseContributionExpressions } from '../../../../../shared/src/api/client/services/contribution'

export function registerSearchHelperSampleContributions({
    extensionsController,
}: ExtensionsControllerProps<'services'>): Unsubscribable {
    const subscriptions = new Subscription()

    const FORM_ID = 'searchHelperSample.form'
    const FORM_SUBMIT_COMMAND_ID = 'searchHelperSample.submit'
    const VERSIONS = ['latest', 'v21', 'v20', 'v19', 'v18'] as const
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
                                    default: 'foo',
                                },
                                version: {
                                    type: 'string',
                                    enum: VERSIONS,
                                    default: VERSIONS[0],
                                },
                            },
                        },
                        submit: {
                            command: FORM_SUBMIT_COMMAND_ID,
                            label: 'Search',
                        },
                    },
                ],
                views: [{ id: 'searchHelperSample.view', form: FORM_ID }],
            }),
        })
    )

    interface FormValue {
        query: string
        version: typeof VERSIONS[number]
    }
    subscriptions.add(
        extensionsController.services.commands.registerCommand({
            command: FORM_SUBMIT_COMMAND_ID,
            run: async (value: FormValue): Promise<void> => {
                console.log(value)
            },
        })
    )

    return subscriptions
}

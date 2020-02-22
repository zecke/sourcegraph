import React, { useCallback } from 'react'
import { ExtensionsControllerProps } from '../../../../../shared/src/extensions/controller'
import Form, { ISubmitEvent } from 'react-jsonschema-form'
import { FormContribution } from '../../../../../shared/src/api/protocol'

interface Props extends ExtensionsControllerProps<'services'> {
    form: FormContribution
}

type FormData = object

/**
 * A form that is displayed in a view (contributed by an extension).
 */
export const ViewForm: React.FunctionComponent<Props> = ({ form, extensionsController }) => {
    // const [formData, setFormData] = useState<FormData>()
    // const onFormChange = useCallback((e: IChangeEvent<FormData>) => setFormData(e.formData), [])
    const onFormSubmit = useCallback(
        (e: ISubmitEvent<FormData>) => {
            // eslint-disable-next-line @typescript-eslint/no-floating-promises
            ;(async () =>
                extensionsController.services.commands.executeCommand({
                    command: form?.submit.command,
                    arguments: [e.formData],
                }))()
        },
        [extensionsController.services.commands, form]
    )

    return <Form<FormData> schema={form?.schema} onSubmit={onFormSubmit} />
}

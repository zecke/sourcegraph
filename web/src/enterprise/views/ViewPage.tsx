import React, { useMemo } from 'react'
import { ExtensionsControllerProps } from '../../../../shared/src/extensions/controller'
import { useView } from './useView'
import { ViewForm } from './forms/ViewForm'

interface Props extends ExtensionsControllerProps<'services'> {
    viewID: string
}

/**
 * A page that displays a single view (contributed by an extension).
 */
export const ViewPage: React.FunctionComponent<Props> = ({ viewID, extensionsController }) => {
    const data = useView(
        viewID,
        useMemo(() => extensionsController.services.contribution.getContributions(), [
            extensionsController.services.contribution,
        ])
    )

    if (data === undefined) {
        return null
    }
    if (data === null) {
        return (
            <div className="alert alert-danger">
                View not found: <code>{viewID}</code>
            </div>
        )
    }

    const { view, form } = data
    return (
        <div>
            <h1>{view.id}</h1>
            {form === undefined ? null : form === null ? (
                <div className="alert alert-danger">
                    View form not found: <code>{view.form}</code>
                </div>
            ) : (
                <ViewForm form={form} extensionsController={extensionsController} />
            )}
        </div>
    )
}

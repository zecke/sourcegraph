import React, { useMemo } from 'react'
import { ExtensionsControllerProps } from '../../../../shared/src/extensions/controller'
import { useView } from './useView'

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

    if (form === null) {
        return (
            <div className="alert alert-danger">
                View form not found: <code>{view.form}</code>
            </div>
        )
    }

    return (
        <div>
            <h1>{view.id}</h1>
            <p>{form?.id}</p>
        </div>
    )
}

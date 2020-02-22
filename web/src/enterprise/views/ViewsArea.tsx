import React from 'react'
import { Switch, Route, RouteComponentProps } from 'react-router'
import { ViewPage } from './ViewPage'
import { ExtensionsControllerProps } from '../../../../shared/src/extensions/controller'

interface Props extends RouteComponentProps<{}>, ExtensionsControllerProps {}

/**
 * The area that handles /views routes, displaying the requested view (contributed by an extension)
 * if it exists.
 */
export const ViewsArea: React.FunctionComponent<Props> = ({ match, ...outerProps }) => (
    <div className="container mt-4">
        {/* eslint-disable react/jsx-no-bind */}
        <Switch>
            <Route path={match.url} exact={true}>
                <div className="alert alert-info">No view specified in the URL.</div>
            </Route>
            <Route
                path={`${match.url}/:view`}
                render={({ match, ...props }: RouteComponentProps<{ view: string }>) => (
                    <ViewPage {...outerProps} {...props} viewID={match.params.view} />
                )}
                exact={true}
            />
        </Switch>
        {/* eslint-enable react/jsx-no-bind */}
    </div>
)

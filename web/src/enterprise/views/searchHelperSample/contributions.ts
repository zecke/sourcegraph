import { Subscription, Unsubscribable, BehaviorSubject, from, combineLatest, of } from 'rxjs'
import { escapeRegExp } from 'lodash'
import { ExtensionsControllerProps } from '../../../../../shared/src/extensions/controller'
import { parseContributionExpressions } from '../../../../../shared/src/api/client/services/contribution'
import { map, switchMap } from 'rxjs/operators'

const VERSIONS = ['latest', 'v21', 'v20', 'v19'] as const

type Version = typeof VERSIONS[number]

const VERSION_TO_REPO_COMMITS: Record<Version, { [repo: string]: string }> = {
    latest: {
        'github.com/sd9/create-extension': 'HEAD',
        'github.com/sd9/dynomite-manager': 'HEAD',
        'github.com/sd9/eslint-formatter-lsif': 'HEAD',
        'github.com/sd9/go-imports-search': 'HEAD',
    },
    v21: {
        'github.com/sd9/create-extension': '4b941f1',
        'github.com/sd9/dynomite-manager': '8d88389',
        'github.com/sd9/eslint-formatter-lsif': '0c7c2d2',
        'github.com/sd9/go-imports-search': '0c8142f',
    },
    v20: {
        'github.com/sd9/create-extension': 'ef00b45',
        'github.com/sd9/dynomite-manager': 'd0c1b79',
        'github.com/sd9/eslint-formatter-lsif': 'a432870',
        'github.com/sd9/go-imports-search': 'b8e24a4',
    },
    v19: {
        'github.com/sd9/create-extension': '7982969',
        'github.com/sd9/dynomite-manager': 'c9eccaa',
        'github.com/sd9/eslint-formatter-lsif': '3d0b413',
        'github.com/sd9/go-imports-search': '759e8e1',
    },
}

// eslint-disable-next-line @typescript-eslint/require-await
const queryReposAndCommitsForReleaseVersion = async (version: Version): Promise<{ [repo: string]: string }> =>
    VERSION_TO_REPO_COMMITS[version]

export function registerSearchHelperSampleContributions({
    extensionsController,
}: ExtensionsControllerProps<'services'>): Unsubscribable {
    const subscriptions = new Subscription()

    const VIEW_ID = 'searchHelperSample.view'
    const FORM_ID = 'searchHelperSample.form'
    const FORM_SUBMIT_COMMAND_ID = 'searchHelperSample.submit'
    const FORM_CHANGE_COMMAND_ID = 'searchHelperSample.change'
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
                                    title: 'Query',
                                    description: 'Supports regexp and other Sourcegraph search query syntax',
                                    type: 'string',
                                    default: 'foo',
                                },
                                version: {
                                    title: 'Release version',
                                    description:
                                        'Search in the commit in each repository that corresponds to the selected release version',
                                    type: 'string',
                                    enum: VERSIONS,
                                    default: VERSIONS[0],
                                },
                                language: {
                                    title: 'Language',
                                    type: 'string',
                                    enum: ['', 'java', 'python', 'javascript', 'cpp'],
                                    enumNames: ['All', 'Java', 'Python', 'JavaScript', 'C++'],
                                    default: '',
                                },
                            },
                        },
                        submit: {
                            command: FORM_SUBMIT_COMMAND_ID,
                            label: 'Search',
                        },
                        change: FORM_CHANGE_COMMAND_ID,
                    },
                ],
                views: [{ id: VIEW_ID, title: 'Acme Corp release search', form: FORM_ID }],
            }),
        })
    )

    interface FormValue {
        query: string
        version: Version
        language: string
    }
    const formValue = new BehaviorSubject<FormValue | undefined>(undefined)
    subscriptions.add(
        extensionsController.services.commands.registerCommand({
            command: FORM_SUBMIT_COMMAND_ID,
            run: async (value: FormValue) => {
                const repoCommits = await queryReposAndCommitsForReleaseVersion(value.version)
                const repoCommitFilters = Object.entries(repoCommits).map(
                    ([repo, commit]) => `repo:^${escapeRegExp(repo)}$@${commit}`
                )
                const langFilters = value.language ? [`lang:${value.language}`] : []
                await extensionsController.services.commands.executeCommand({
                    command: 'open',
                    arguments: [
                        `/search?q=${encodeURIComponent(
                            [value.query, ...langFilters, ...repoCommitFilters].join(' ')
                        )}`,
                    ],
                })
            },
        })
    )
    subscriptions.add(
        extensionsController.services.commands.registerCommand({
            command: FORM_CHANGE_COMMAND_ID,
            run: (value: FormValue): Promise<void> => {
                formValue.next(value)
                return Promise.resolve()
            },
        })
    )

    subscriptions.add(
        extensionsController.services.views.registerProvider(
            { container: '' as any, id: VIEW_ID },
            combineLatest([
                formValue,
                formValue.pipe(
                    switchMap(value => (value ? from(queryReposAndCommitsForReleaseVersion(value?.version)) : of(null)))
                ),
            ]).pipe(
                map(([value, repoCommits]) =>
                    value && repoCommits
                        ? {
                              title: `Release version ${value?.version}`,
                              content:
                                  'Searching repositories at versions:\n\n' +
                                  Object.entries(repoCommits)
                                      .map(([repo, commit]) => `- ${repo} @ ${commit}`)
                                      .join('\n'),
                              priority: 0,
                          }
                        : { title: 'Loading...', content: '', priority: 0 }
                )
            )
        )
    )

    return subscriptions
}

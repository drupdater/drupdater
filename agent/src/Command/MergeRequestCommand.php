<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Drupdater\Agent\Failure;
use Drupdater\Agent\MergeRequest\MergeRequest;

/**
 * Renders the run's merge request from its report. Needs no project: the report is everything it
 * may say.
 *
 * Request: {report}, the run report as published. Result: {title, description}.
 */
final class MergeRequestCommand extends AgentCommand
{
    private readonly MergeRequest $mergeRequest;

    public function __construct(?MergeRequest $mergeRequest = null)
    {
        $this->mergeRequest = $mergeRequest ?? MergeRequest::builtin();
        parent::__construct();
    }

    protected function configure(): void
    {
        $this->setName('drupdater:merge-request')
            ->setDescription('Renders the merge request from the run report.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        $report = $this->rawMember('report');
        if (!$report instanceof \stdClass) {
            throw new Failure(Failure::INVALID_REQUEST, 'report must be an object');
        }
        $addons = $report->addons ?? new \stdClass();
        if (!$addons instanceof \stdClass) {
            throw new Failure(Failure::INVALID_REQUEST, 'the report\'s addons must be an object');
        }
        return [
            'title' => MergeRequest::title($report),
            'description' => $this->mergeRequest->description($addons),
        ];
    }
}

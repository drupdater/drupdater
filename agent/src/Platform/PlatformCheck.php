<?php

declare(strict_types=1);

namespace Drupdater\Agent\Platform;

use Composer\Composer;
use Composer\Package\Link;
use Composer\Package\PackageInterface;
use Composer\Repository\InstalledRepository;
use Composer\Repository\PlatformRepository;
use Composer\Repository\RepositoryInterface;
use Composer\Repository\RootPackageRepository;
use Composer\Semver\Constraint\Constraint;
use Composer\Semver\Constraint\ConstraintInterface;

/**
 * Composer's CheckPlatformReqsCommand with `--lock`, answering in data instead of a table.
 */
final class PlatformCheck
{
    public function __construct(private readonly ?RepositoryInterface $platform = null)
    {
    }

    /**
     * @return list<array{name: string, version: string|null, constraint: string, required_by: string}>
     */
    public function unmet(Composer $composer): array
    {
        $installed = new InstalledRepository([
            $composer->getLocker()->getLockedRepository(true),
            new RootPackageRepository(clone $composer->getPackage()),
        ]);

        $requires = [];
        foreach ($composer->getPackage()->getDevRequires() as $name => $link) {
            $requires[$name][] = $link;
        }
        foreach ($installed->getPackages() as $package) {
            foreach ($package->getRequires() as $name => $link) {
                $requires[$name][] = $link;
            }
        }
        ksort($requires);

        $installed->addRepository($this->platform ?? new PlatformRepository([], []));

        $unmet = [];
        foreach ($requires as $name => $links) {
            if (!PlatformRepository::isPlatformPackage($name)) {
                continue;
            }
            $failure = self::check($installed, $name, $links);
            if ($failure !== null) {
                $unmet[] = $failure;
            }
        }
        return $unmet;
    }

    /**
     * Met when one candidate satisfies every link; otherwise the first candidate's first miss.
     *
     * @param non-empty-list<Link> $links
     * @return array{name: string, version: string|null, constraint: string, required_by: string}|null
     */
    private static function check(InstalledRepository $installed, string $name, array $links): ?array
    {
        $firstMiss = null;
        foreach ($installed->findPackagesWithReplacersAndProviders($name) as $candidate) {
            $provided = self::provided($candidate, $name);
            if ($provided === null) {
                continue;
            }
            $miss = self::firstMiss($links, $provided);
            if ($miss === null) {
                return null;
            }
            $firstMiss ??= self::row($name, $provided->getPrettyString(), $miss);
        }
        return $firstMiss ?? self::row($name, null, $links[0]);
    }

    /**
     * @param list<Link> $links
     */
    private static function firstMiss(array $links, ConstraintInterface $provided): ?Link
    {
        foreach ($links as $link) {
            if (!$link->getConstraint()->matches($provided)) {
                return $link;
            }
        }
        return null;
    }

    private static function provided(PackageInterface $candidate, string $name): ?ConstraintInterface
    {
        if ($candidate->getName() === $name) {
            $constraint = new Constraint('=', $candidate->getVersion());
            $constraint->setPrettyString($candidate->getPrettyVersion());
            return $constraint;
        }
        foreach ([...$candidate->getProvides(), ...$candidate->getReplaces()] as $link) {
            if ($link->getTarget() === $name) {
                return $link->getConstraint();
            }
        }
        return null;
    }

    /**
     * @return array{name: string, version: string|null, constraint: string, required_by: string}
     */
    private static function row(string $name, ?string $version, Link $link): array
    {
        return ['name' => $name, 'version' => $version, 'constraint' => $link->getPrettyConstraint(), 'required_by' => $link->getSource()];
    }
}

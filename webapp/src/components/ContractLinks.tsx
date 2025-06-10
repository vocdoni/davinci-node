import {
  Box,
  Heading,
  VStack,
  HStack,
  Text,
  Link,
  Skeleton,
  Badge,
  Card,
  CardBody,
} from '@chakra-ui/react'
import { FaExternalLinkAlt } from 'react-icons/fa'
import { ContractAddresses } from '~types/api'

interface ContractLinksProps {
  contracts?: ContractAddresses
  isLoading: boolean
}

export const ContractLinks = ({ contracts, isLoading }: ContractLinksProps) => {
  const blockExplorerUrl = import.meta.env.BLOCK_EXPLORER_URL || 'https://sepolia.etherscan.io/address'

  const contractItems = [
    { label: 'Process Registry', address: contracts?.process },
    { label: 'Organization Registry', address: contracts?.organization },
  ]

  return (
    <Card>
      <CardBody>
        <VStack align="stretch" spacing={4}>
          <Heading size="md" color="gray.700">
            Smart Contracts
          </Heading>
          <VStack align="stretch" spacing={3}>
            {contractItems.map((item) => (
              <HStack key={item.label} justify="space-between">
                <Text fontWeight="medium" color="gray.600">
                  {item.label}:
                </Text>
                {isLoading ? (
                  <Skeleton height="20px" width="200px" />
                ) : item.address ? (
                  <HStack>
                    <Badge colorScheme="purple" fontFamily="mono" fontSize="xs">
                      {item.address.slice(0, 6)}...{item.address.slice(-4)}
                    </Badge>
                    <Link
                      href={`${blockExplorerUrl}/${item.address}`}
                      isExternal
                      color="purple.500"
                      _hover={{ color: 'purple.600' }}
                    >
                      <FaExternalLinkAlt size={12} />
                    </Link>
                  </HStack>
                ) : (
                  <Text color="gray.400">Not available</Text>
                )}
              </HStack>
            ))}
          </VStack>
        </VStack>
      </CardBody>
    </Card>
  )
}
